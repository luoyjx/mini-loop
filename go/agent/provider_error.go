package agent

import "errors"

// ProviderErrorDetail projects a bounded user-facing diagnostic without coupling
// Session to a concrete transport or changing generic lifecycle diagnostics.
type ProviderErrorDetail interface {
	ProviderErrorDetail() (string, bool)
}

func providerRunError(err error) string {
	var source ProviderErrorDetail
	if errors.As(err, &source) {
		if detail, observed := source.ProviderErrorDetail(); observed {
			return truncateRunes(detail, 500)
		}
	}
	return boundedError(err)
}
