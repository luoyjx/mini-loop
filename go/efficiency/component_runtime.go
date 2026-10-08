package efficiency

// DescriptorComponent is the common named contract for staged component SPIs.
// Hook capabilities are snapshotted separately; domain payloads stay stage-specific.
type DescriptorComponent interface{ Descriptor() Descriptor }
type componentBinding struct {
	descriptor  DescriptorConfig
	mode        Mode
	serial      chan struct{}
	initializer ComponentInitializer
	health      ComponentHealthProvider
	closer      ComponentCloser
}

func newComponentBinding(component DescriptorComponent, d DescriptorConfig, mode Mode) *componentBinding {
	b := &componentBinding{descriptor: d, mode: mode, serial: make(chan struct{}, 1)}
	b.initializer, _ = component.(ComponentInitializer)
	b.health, _ = component.(ComponentHealthProvider)
	b.closer, _ = component.(ComponentCloser)
	return b
}

type receiptReferences struct{ ref, digest *string }

type componentRuntime struct {
	bindings  []*componentBinding
	lifecycle *lifecycleState
	counter   *componentCounter
}

func newComponentRuntime(counter TokenCounter) *componentRuntime {
	return &componentRuntime{lifecycle: newLifecycleState(), counter: &componentCounter{call: counter}}
}
