package vcs

// MockProvider is a configurable Provider for use in tests.
// Unset function fields return zero values with no error.
type MockProvider struct {
	CreatePRFn             func(title, body string, draft bool, repoDir string) (string, error)
	GetPRMetadataFn        func(prNum int, repoDir string) ([]byte, error)
	GetPRReviewsFn         func(prNum int, repoDir string) ([]byte, error)
	GetPRCommentsFn        func(prNum int, repoDir string) ([]byte, error)
	MarkPRReadyFn          func(prNum int, repoDir string) error
	ClosePRFn              func(prNum int, repoDir string) error
	OpenPRInBrowserFn      func(prNum int, repoDir string) error
	GetPRHeadBranchFn      func(prNum int, repoDir string) (string, error)
	GetPRStateJSONFn       func(prNum int, repoDir string) ([]byte, error)
	GetReviewCommentsRawFn func(prNum int, repoDir string) ([]byte, error)
	FindPRByBranchFn       func(branch, repoDir string) (int, bool, error)
}

func (m *MockProvider) CreatePR(title, body string, draft bool, repoDir string) (string, error) {
	if m.CreatePRFn != nil {
		return m.CreatePRFn(title, body, draft, repoDir)
	}
	return "", nil
}

func (m *MockProvider) GetPRMetadata(prNum int, repoDir string) ([]byte, error) {
	if m.GetPRMetadataFn != nil {
		return m.GetPRMetadataFn(prNum, repoDir)
	}
	return nil, nil
}

func (m *MockProvider) GetPRReviews(prNum int, repoDir string) ([]byte, error) {
	if m.GetPRReviewsFn != nil {
		return m.GetPRReviewsFn(prNum, repoDir)
	}
	return nil, nil
}

func (m *MockProvider) GetPRComments(prNum int, repoDir string) ([]byte, error) {
	if m.GetPRCommentsFn != nil {
		return m.GetPRCommentsFn(prNum, repoDir)
	}
	return nil, nil
}

func (m *MockProvider) MarkPRReady(prNum int, repoDir string) error {
	if m.MarkPRReadyFn != nil {
		return m.MarkPRReadyFn(prNum, repoDir)
	}
	return nil
}

func (m *MockProvider) ClosePR(prNum int, repoDir string) error {
	if m.ClosePRFn != nil {
		return m.ClosePRFn(prNum, repoDir)
	}
	return nil
}

func (m *MockProvider) OpenPRInBrowser(prNum int, repoDir string) error {
	if m.OpenPRInBrowserFn != nil {
		return m.OpenPRInBrowserFn(prNum, repoDir)
	}
	return nil
}

func (m *MockProvider) GetPRHeadBranch(prNum int, repoDir string) (string, error) {
	if m.GetPRHeadBranchFn != nil {
		return m.GetPRHeadBranchFn(prNum, repoDir)
	}
	return "", nil
}

func (m *MockProvider) GetPRStateJSON(prNum int, repoDir string) ([]byte, error) {
	if m.GetPRStateJSONFn != nil {
		return m.GetPRStateJSONFn(prNum, repoDir)
	}
	return nil, nil
}

func (m *MockProvider) GetReviewCommentsRaw(prNum int, repoDir string) ([]byte, error) {
	if m.GetReviewCommentsRawFn != nil {
		return m.GetReviewCommentsRawFn(prNum, repoDir)
	}
	return nil, nil
}

func (m *MockProvider) FindPRByBranch(branch, repoDir string) (int, bool, error) {
	if m.FindPRByBranchFn != nil {
		return m.FindPRByBranchFn(branch, repoDir)
	}
	return 0, false, nil
}
