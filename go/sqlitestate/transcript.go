package sqlitestate

import (
	"context"
	"errors"
	"math"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

var _ agent.TranscriptStore = (*Store)(nil)
var ErrOrdinalRange = errors.New("SQLite ordinal exceeds its signed integer range")

func (s *Store) maxOrdinal(ctx context.Context, table tableName, id agent.SessionID) (int64, error) {
	var head int64
	err := s.conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(ordinal),0) FROM "+string(table)+" WHERE session_id=?", id).Scan(&head)
	return head, err
}

func (s *Store) epoch(ctx context.Context, id agent.SessionID) (agent.TranscriptEpoch, error) {
	var epoch agent.TranscriptEpoch
	err := s.conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(epoch),0) FROM messages WHERE session_id=?", id).Scan(&epoch)
	return epoch, err
}

func (s *Store) messageCount(ctx context.Context, id agent.SessionID, epoch agent.TranscriptEpoch) (int, error) {
	if epoch == 0 {
		return 0, nil
	}
	var count int
	err := s.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE session_id=? AND epoch=?", id, epoch).Scan(&count)
	return count, err
}

func (s *Store) AppendMessages(ctx context.Context, id agent.SessionID, messages []protocol.Message, epoch agent.TranscriptEpoch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if err := validateText(string(id)); err != nil {
		return 0, err
	}
	if len(messages) == 0 {
		return s.messageCount(ctx, id, epoch)
	}
	err := s.withImmediate(ctx, func() error {
		head, err := s.maxOrdinal(ctx, messagesTable, id)
		if err != nil {
			return err
		}
		if head > math.MaxInt64-int64(len(messages)) {
			return ErrOrdinalRange
		}
		for offset, message := range messages {
			if err := validateMessageText(message); err != nil {
				return err
			}
			data, err := message.MarshalArchiveJSON()
			if err != nil {
				return err
			}
			payload, err := sourcePayload(data)
			if err != nil {
				return err
			}
			if _, err := s.conn.ExecContext(ctx, "INSERT INTO messages (session_id,ordinal,epoch,payload) VALUES (?,?,?,?)", id, head+int64(offset)+1, epoch, payload); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return s.messageCount(ctx, id, epoch)
}

func (s *Store) TranscriptEpoch(ctx context.Context, id agent.SessionID) (agent.TranscriptEpoch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if err := validateText(string(id)); err != nil {
		return 0, err
	}
	return s.epoch(ctx, id)
}

func (s *Store) MessageCount(ctx context.Context, id agent.SessionID, selected *agent.TranscriptEpoch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if err := validateText(string(id)); err != nil {
		return 0, err
	}
	epoch, err := s.selectEpoch(ctx, id, selected)
	if err != nil {
		return 0, err
	}
	return s.messageCount(ctx, id, epoch)
}

func (s *Store) selectEpoch(ctx context.Context, id agent.SessionID, selected *agent.TranscriptEpoch) (agent.TranscriptEpoch, error) {
	if selected != nil {
		return *selected, nil
	}
	return s.epoch(ctx, id)
}

func (s *Store) LoadMessages(ctx context.Context, id agent.SessionID, selected *agent.TranscriptEpoch) ([]protocol.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validateText(string(id)); err != nil {
		return nil, err
	}
	epoch, err := s.selectEpoch(ctx, id, selected)
	if err != nil {
		return nil, err
	}
	result := []protocol.Message{}
	if epoch == 0 {
		return result, nil
	}
	rows, err := s.conn.QueryContext(ctx, "SELECT payload FROM messages WHERE session_id=? AND epoch=? ORDER BY ordinal", id, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		message, err := protocol.DecodeStoredMessage([]byte(payload))
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, rows.Err()
}

func validateMessageText(message protocol.Message) error {
	if plain, ok := message.Content.Plain(); ok {
		return validateText(plain)
	}
	blocks, _ := message.Content.Blocks()
	for _, block := range blocks {
		var err error
		switch block.Kind() {
		case protocol.BlockTextEditorToolResult:
			value, _ := block.TextEditorToolResult()
			err = validateText(value.ToolUseID)
			if result, ok := value.Content.View(); ok && err == nil {
				err = validateText(result.Content, string(result.FileType))
			} else if result, ok := value.Content.Replace(); ok && err == nil && result.Lines != nil {
				for _, line := range *result.Lines {
					if err != nil {
						break
					}
					err = validateText(line)
				}
			} else if failure, ok := value.Content.Error(); ok && err == nil {
				err = validateText(string(failure.Code))
				if err == nil && failure.Message != nil {
					err = validateText(*failure.Message)
				}
			}

		case protocol.BlockBashExecutionToolResult:
			value, _ := block.BashExecutionToolResult()
			err = validateText(value.ToolUseID)
			if result, ok := value.Content.Result(); ok && err == nil {
				err = validateText(result.Stdout, result.Stderr)
				for _, file := range result.OutputFiles {
					if err != nil {
						break
					}
					err = validateText(file.FileID)
				}
			} else if failure, ok := value.Content.Error(); ok && err == nil {
				err = validateText(string(failure.Code))
			}

		case protocol.BlockCodeExecutionToolResult:
			value, _ := block.CodeExecutionToolResult()
			err = validateText(value.ToolUseID)
			if result, ok := value.Content.Plain(); ok && err == nil {
				err = validateText(result.Stdout, result.Stderr)
				for _, file := range result.OutputFiles {
					if err != nil {
						break
					}
					err = validateText(file.FileID)
				}
			} else if result, ok := value.Content.Encrypted(); ok && err == nil {
				err = validateText(result.EncryptedStdout, result.Stderr)
				for _, file := range result.OutputFiles {
					if err != nil {
						break
					}
					err = validateText(file.FileID)
				}
			} else if failure, ok := value.Content.Error(); ok && err == nil {
				err = validateText(string(failure.Code))
			}

		case protocol.BlockWebFetchToolResult:
			value, _ := block.WebFetchToolResult()
			err = validateText(value.ToolUseID)
			if err == nil && value.Caller != nil {
				err = validateText(value.Caller.ToolID())
			}
			if result, ok := value.Content.Result(); ok && err == nil {
				err = validateText(result.URL, result.Document.Source.Data, string(result.Document.Source.MediaType))
				if err == nil && result.RetrievedAt != nil {
					err = validateText(*result.RetrievedAt)
				}
				if err == nil && result.Document.Title != nil {
					err = validateText(*result.Document.Title)
				}
			} else if failure, ok := value.Content.Error(); ok && err == nil {
				err = validateText(string(failure.Code))
			}

		case protocol.BlockServerToolUse:
			value, _ := block.ServerToolUse()
			err = validateText(value.ID, string(value.Name))
			if err == nil && value.Caller != nil {
				err = validateText(value.Caller.ToolID())
			}

		case protocol.BlockToolSearchToolResult:
			value, _ := block.ToolSearchToolResult()
			err = validateText(value.ToolUseID)
			if results, ok := value.Content.Results(); ok {
				for _, ref := range results.References {
					if err != nil {
						break
					}
					err = validateText(string(ref.Name))
				}
			} else if failure, ok := value.Content.Error(); ok && err == nil {
				err = validateText(string(failure.Code))
				if err == nil && failure.Message != nil {
					err = validateText(*failure.Message)
				}
			}
		case protocol.BlockWebSearchToolResult:
			value, _ := block.WebSearchToolResult()
			err = validateText(value.ToolUseID)
			if err == nil && value.Caller != nil {
				err = validateText(value.Caller.ToolID())
			}
			if results, ok := value.Content.Results(); ok {
				for _, row := range results {
					if err != nil {
						break
					}
					err = validateText(row.Title, row.URL, row.EncryptedContent)
					if err == nil && row.PageAge != nil {
						err = validateText(*row.PageAge)
					}
				}
			} else if failure, ok := value.Content.Error(); ok && err == nil {
				err = validateText(string(failure.Code))
			}
		case protocol.BlockContainerUpload:
			value, _ := block.ContainerUpload()
			err = validateText(value.FileID)
		case protocol.BlockText:
			value, _ := block.Text()
			err = validateText(value.Text)
		case protocol.BlockThinking:
			value, _ := block.Thinking()
			err = validateText(value.Thinking, value.Signature)
		case protocol.BlockRedactedThinking:
			value, _ := block.RedactedThinking()
			err = validateText(value.Data)
		case protocol.BlockToolResult:
			value, _ := block.ToolResult()
			err = validateText(value.ToolUseID, value.Content)
		case protocol.BlockToolUse:
			value, _ := block.ToolUse()
			err = validateText(value.ID, string(value.Name))
			if err == nil && value.Caller != nil {
				err = validateText(value.Caller.ToolID())
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}
