package efficiency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"sync"
	"testing"
)

type timeoutRecord struct{ Status, Reason string }

func TestTimeoutConversionMatchesActualSourceStagesAndLifecycle(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-timeouts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			TimeoutMS       *big.Int `json:"timeout_ms"`
			Operation       string
			Events          []string
			Records         []timeoutRecord
			Error           *sourceFault
			AdmissionReason string `json:"admission_reason"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 25 {
		t.Fatal("Source timeout inventory drift")
	}
	for index, row := range fixture.Rows {
		t.Run(fmt.Sprintf("%d-%s", index, row.Operation), func(t *testing.T) {
			stage := Observation
			if row.Operation == "request" {
				stage = RequestContext
			}
			if row.Operation == "response" {
				stage = ResponsePolicy
			}
			descriptor := DefaultDescriptor("timeout-probe", "1", stage)
			descriptor.TimeoutMS = row.TimeoutMS
			d, err := NewDescriptor(descriptor)
			if err != nil {
				t.Fatal(err)
			}
			events := [][2]string{}
			var mu sync.Mutex
			probe := &registryProbe{descriptor: d, events: &events, mu: &mu}
			var registry Registry
			switch stage {
			case Observation:
				_, err = registry.RegisterObservation(probe, nil)
			case RequestContext:
				_, err = registry.RegisterRequest(probe, nil)
			case ResponsePolicy:
				_, err = registry.RegisterResponse(probe, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg := DefaultRuntimeConfig()
			cfg.DefaultMode = Enforce
			runtime, err := registry.Runtime(cfg)
			if err != nil {
				t.Fatal(err)
			}
			records := []timeoutRecord{}
			var callErr error
			appendReceipts := func(receipts []ReceiptProjection) {
				for _, receipt := range receipts {
					records = append(records, timeoutRecord{string(receipt.Status), receipt.Reason})
				}
			}
			appendLifecycle := func(report LifecycleReport, err error) {
				callErr = err
				if err == nil {
					view, e := report.Projection()
					if e != nil {
						t.Fatal(e)
					}
					for _, record := range view.Components {
						records = append(records, timeoutRecord{string(record.Status), record.Detail})
					}
				}
			}
			switch row.Operation {
			case "observation":
				observation, _ := NewObservation(DefaultObservation("text"))
				out, e := runtime.ReduceObservation(context.Background(), observation, ReductionOptions{}, false)
				callErr = e
				if e == nil {
					view, e := out.Snapshot()
					if e != nil {
						t.Fatal(e)
					}
					appendReceipts(view.Receipts)
				}
			case "request":
				request, _ := NewOptimizationContext(OptimizationContextConfig{Request: requestDocument(t, `{"messages":[]}`)})
				out, e := runtime.OptimizeRequest(context.Background(), request, RequestOptimizationOptions{})
				callErr = e
				if e == nil {
					view, e := out.Snapshot()
					if e != nil {
						t.Fatal(e)
					}
					appendReceipts(view.Receipts)
				}
			case "response":
				response, _ := NewResponseContext(DefaultResponseContext("task"))
				out, e := runtime.PlanResponse(context.Background(), response)
				callErr = e
				if e == nil {
					view, e := out.Snapshot()
					if e != nil {
						t.Fatal(e)
					}
					appendReceipts(view.Receipts)
				}
			case "initialize":
				appendLifecycle(runtime.Initialize(context.Background(), LifecycleServices{}))
			case "close":
				appendLifecycle(runtime.Close(context.Background(), LifecycleCloseOptions{}))
			default:
				t.Fatal("unknown Source operation", row.Operation)
			}
			var fault *sourceFault
			if callErr != nil {
				var component *ComponentFault
				if !errors.As(callErr, &component) {
					t.Fatal("untyped timeout escape", callErr)
				}
				fault = &sourceFault{component.Class, component.Error()}
			}
			observed := []string{}
			for _, event := range events {
				observed = append(observed, event[1])
			}
			if !reflect.DeepEqual(observed, row.Events) || !reflect.DeepEqual(records, row.Records) || !reflect.DeepEqual(fault, row.Error) {
				t.Fatal("Source timeout mismatch", observed, records, fault, row.Events, row.Records, row.Error)
			}
			if row.Operation == "close" {
				observation, _ := NewObservation(DefaultObservation("text"))
				out, e := runtime.ReduceObservation(context.Background(), observation, ReductionOptions{}, false)
				if e != nil {
					t.Fatal(e)
				}
				view, e := out.Snapshot()
				if e != nil || len(view.Receipts) != 1 || view.Receipts[0].Reason != row.AdmissionReason {
					t.Fatal("close overflow reopened admission", view, e)
				}
			}
		})
	}
}
