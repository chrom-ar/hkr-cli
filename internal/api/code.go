package api

// Repo is a connected repository.
type Repo struct {
	ID                string   `json:"id"`
	Provider          string   `json:"provider"`
	FullName          string   `json:"full_name"`
	DefaultBranch     string   `json:"default_branch"`
	Private           bool     `json:"private"`
	Archived          bool     `json:"archived"`
	ReviewOnPush      bool     `json:"review_on_push"`
	ReviewBranches    []string `json:"review_branches"`
	ReviewPaths       []string `json:"review_paths"`
	ReviewMinMinutes  int      `json:"review_min_minutes"`
	WatchDependencies bool     `json:"watch_dependencies"`
	FixLevel          string   `json:"fix_level"`
	LastSyncedAt      string   `json:"last_synced_at"`
	ReviewedAt        string   `json:"reviewed_at"`
	URL               string   `json:"url"`
	// PushHook says, after a change that turned reviews on, whether HKr
	// could add the push webhook: ok, insufficient_access or failed.
	PushHook string `json:"push_hook,omitempty"`
}

// Review is a push review. Comments is a count in lists; one review
// (ReviewDetail) has them in full.
type Review struct {
	ID                string `json:"id"`
	RepositoryID      string `json:"repository_id"`
	Status            string `json:"status"`
	Reason            string `json:"reason"`
	Branch            string `json:"branch"`
	CommitSHA         string `json:"commit_sha"`
	PullRequestNumber int    `json:"pull_request_number"`
	Summary           string `json:"summary"`
	Comments          int    `json:"comments"`
	CreatedAt         string `json:"created_at"`
	StartedAt         string `json:"started_at"`
	FinishedAt        string `json:"finished_at"`
	URL               string `json:"url"`
}

// ReviewDetail is one review with its comments.
type ReviewDetail struct {
	Review
	Comments []ReviewComment `json:"comments"`
}

// ReviewComment is what a review said about one line.
type ReviewComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Severity  string `json:"severity"`
	Class     string `json:"class"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	FindingID string `json:"finding_id"`
	PostedAt  string `json:"posted_at"`
}

// Exposure is a dependency CVE in a repository.
type Exposure struct {
	ID              string   `json:"id"`
	Status          string   `json:"status"`
	Repository      RepoRef  `json:"repository"`
	Path            string   `json:"path"`
	Ecosystem       string   `json:"ecosystem"`
	PackageName     string   `json:"package_name"`
	MatchedVersion  string   `json:"matched_version"`
	FixedVersion    string   `json:"fixed_version"`
	Scope           string   `json:"scope"`
	MatchConfidence string   `json:"match_confidence"`
	Advisory        Advisory `json:"advisory"`
	Fix             *Fix     `json:"fix"`
	Urgent          bool     `json:"urgent"`
	MergeUndone     bool     `json:"merge_undone"`
	CreatedAt       string   `json:"created_at"`
	ResolvedAt      string   `json:"resolved_at"`
}

// Advisory is a published vulnerability.
type Advisory struct {
	ID          string   `json:"id"`
	CVEID       string   `json:"cve_id"`
	OSVID       string   `json:"osv_id"`
	Severity    string   `json:"severity"`
	CVSSScore   *float64 `json:"cvss_score"`
	Summary     string   `json:"summary"`
	PublishedAt string   `json:"published_at"`
}

// Name is the advisory's CVE id, else its OSV id.
func (a Advisory) Name() string {
	if a.CVEID != "" {
		return a.CVEID
	}
	return a.OSVID
}

// Connection is a git host connected to the organization.
type Connection struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

// Link says a repository deploys or defines one of a domain's hosts.
type Link struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Kind       string  `json:"kind"`
	Source     string  `json:"source"`
	Confidence *int    `json:"confidence"`
	Ref        string  `json:"ref"`
	Repository RepoRef `json:"repository"`
	Target     struct {
		ID       string `json:"id"`
		Value    string `json:"value"`
		DomainID string `json:"domain_id"`
	} `json:"target"`
	DecidedBy *User  `json:"decided_by"`
	CreatedAt string `json:"created_at"`
}
