package agent

import "errors"

// SkillCatalogue reads the same fixed source used for this session's model
// requests. It does not resolve the owner's newer publication cache or load a
// skill body. Shared custom sources obey ManagerServices' concurrency contract.
func (session *ManagedSession) SkillCatalogue() (catalogue string, err error) {
	defer func() {
		if recover() != nil {
			catalogue = ""
			err = errors.New("skill catalogue unavailable")
		}
	}()
	if source := session.core.skills; source != nil {
		return source.Descriptions(), nil
	}
	return "", nil
}
