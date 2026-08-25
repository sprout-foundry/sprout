package agent

// RegisterPasswordRequestForTest registers a password request in the broker
// for use by webui tests. Returns the response channel so the test can
// verify delivery. Call CleanupPasswordRequestForTest after the test.
func RegisterPasswordRequestForTest(requestID string) chan string {
	return passwordPrompterBroker.register(requestID)
}

// CleanupPasswordRequestForTest removes a password request from the broker.
func CleanupPasswordRequestForTest(requestID string) {
	passwordPrompterBroker.cleanup(requestID)
}
