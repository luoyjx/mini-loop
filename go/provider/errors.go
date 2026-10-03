package provider

type ErrorClass string

const (
	ClassStatus            ErrorClass = "APIStatusError"
	ClassBadRequest        ErrorClass = "BadRequestError"
	ClassAuthentication    ErrorClass = "AuthenticationError"
	ClassPermission        ErrorClass = "PermissionDeniedError"
	ClassNotFound          ErrorClass = "NotFoundError"
	ClassConflict          ErrorClass = "ConflictError"
	ClassUnprocessable     ErrorClass = "UnprocessableEntityError"
	ClassRateLimit         ErrorClass = "RateLimitError"
	ClassInternal          ErrorClass = "InternalServerError"
	ClassConnection        ErrorClass = "APIConnectionError"
	ClassTimeout           ErrorClass = "APITimeoutError"
	ClassStreamingRequired ErrorClass = "ValueError"
	ClassProtocol          ErrorClass = "ProtocolError"
	ClassWireLimit         ErrorClass = "WireLimitError"
)

func (e *Failure) Class() ErrorClass {
	switch e.Kind {
	case FailureConnection:
		return ClassConnection
	case FailureTimeout:
		return ClassTimeout
	case FailureStreamingRequired:
		return ClassStreamingRequired
	case FailureProtocol:
		return ClassProtocol
	case FailureLimit:
		return ClassWireLimit
	case FailureStatus:
		switch e.Status {
		case 400:
			return ClassBadRequest
		case 401:
			return ClassAuthentication
		case 403:
			return ClassPermission
		case 404:
			return ClassNotFound
		case 409:
			return ClassConflict
		case 422:
			return ClassUnprocessable
		case 429:
			return ClassRateLimit
		default:
			if e.Status >= 500 {
				return ClassInternal
			}
			return ClassStatus
		}
	default:
		return ClassProtocol
	}
}
