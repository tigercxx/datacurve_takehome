package schemas

import "time"

type Pos struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type Range struct {
	Start Pos `json:"start"`
	End   Pos `json:"end"`
}

type BaseEvent struct {
	T         time.Time `json:"t"`
	Type      string    `json:"type"`
	SessionID string    `json:"session_id"`
	Actor     string    `json:"actor,omitempty"`
	Repo      *struct {
		RemoteURL string `json:"remote_url"`
		Branch    string `json:"branch,omitempty"`
		Commit    string `json:"commit,omitempty"`
	} `json:"repo,omitempty"`
	Editor *struct{ Name, Version, OS string } `json:"editor,omitempty"`
	Meta   map[string]any                      `json:"meta,omitempty"`
}

type FileOpened struct {
	BaseEvent
	FilePath string                   `json:"file_path"`
	Language string                   `json:"language,omitempty"`
	FileHash string                   `json:"file_hash,omitempty"`
	Cursor   *struct{ Line, Col int } `json:"cursor,omitempty"`
}

type GoToDefinition struct {
	BaseEvent
	Source struct {
		FilePath string `json:"file_path"`
		Range    *Range `json:"range,omitempty"`
		Symbol   string `json:"symbol"`
	} `json:"source"`
	Target struct {
		FilePath   string `json:"file_path"`
		Line       int    `json:"line,omitempty"`
		Col        int    `json:"col,omitempty"`
		SymbolKind string `json:"symbol_kind,omitempty"`
	} `json:"target"`
	Provider  string `json:"provider,omitempty"`
	LatencyMS int    `json:"latency_ms,omitempty"`
}

type FindReferences struct {
	BaseEvent
	Symbol struct {
		Name       string `json:"name"`
		FilePath   string `json:"file_path"`
		Line       int    `json:"line,omitempty"`
		Col        int    `json:"col,omitempty"`
		SymbolKind string `json:"symbol_kind,omitempty"`
	} `json:"symbol"`
	Provider  string `json:"provider,omitempty"`
	LatencyMS int    `json:"latency_ms,omitempty"`
	Results   []struct {
		FilePath       string `json:"file_path"`
		Line           int    `json:"line"`
		Col            int    `json:"col"`
		ContextSnippet string `json:"context_snippet,omitempty"`
	} `json:"results,omitempty"`
}

type TerminalCommand struct {
	BaseEvent
	Cmd         string   `json:"cmd"`
	Args        []string `json:"args,omitempty"`
	Cwd         string   `json:"cwd,omitempty"`
	EnvKeys     []string `json:"env_keys,omitempty"`
	ExitCode    *int     `json:"exit_code,omitempty"`
	DurationMS  *int     `json:"duration_ms,omitempty"`
	StdoutTrunc string   `json:"stdout_truncated,omitempty"`
	StderrTrunc string   `json:"stderr_truncated,omitempty"`
}

type EditMade struct {
	BaseEvent
	FilePath     string `json:"file_path"`
	Op           string `json:"op"`
	Range        *Range `json:"range,omitempty"`
	PatchUnified string `json:"patch_unified"`
	BeforeHash   string `json:"before_hash,omitempty"`
	AfterHash    string `json:"after_hash,omitempty"`
	EditorAction string `json:"editor_action,omitempty"`
}

type CommitMade struct {
	BaseEvent
	Commit    string                                     `json:"commit"`
	Message   string                                     `json:"message,omitempty"`
	Parent    string                                     `json:"parent,omitempty"`
	DiffStats *struct{ Files, Additions, Deletions int } `json:"diff_stats,omitempty"`
}

type PushRemote struct {
	BaseEvent
	Remote    string   `json:"remote"`
	RemoteURL string   `json:"remote_url,omitempty"`
	Branch    string   `json:"branch"`
	Commits   []string `json:"commits,omitempty"`
	Forced    bool     `json:"forced,omitempty"`
}

type PROpened struct {
	BaseEvent
	Provider string `json:"provider,omitempty"`
	PR       struct {
		ID         string `json:"id,omitempty"`
		URL        string `json:"url"`
		Title      string `json:"title"`
		BaseBranch string `json:"base_branch"`
		HeadBranch string `json:"head_branch"`
		HeadCommit string `json:"head_commit,omitempty"`
	} `json:"pr"`
}

type Thought struct {
	BaseEvent
	Raw      string   `json:"raw,omitempty"`
	Redacted string   `json:"redacted,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type AppendEventsRequest struct {
	Events []map[string]any `json:"events"`
}

type CreateTraceRequest struct {
	Developer   map[string]any `json:"developer"`
	Task        map[string]any `json:"task"`
	Environment map[string]any `json:"environment"`
}

type QaRequest struct {
	TestCommand string `json:"test_command,omitempty"`
	DockerImage string `json:"docker_image,omitempty"`
}

type TraceOut struct {
	TraceID     string         `json:"trace_id"`
	CreatedAt   time.Time      `json:"created_at"`
	Developer   map[string]any `json:"developer"`
	Task        map[string]any `json:"task"`
	Environment map[string]any `json:"environment"`
	Artifacts   map[string]any `json:"artifacts,omitempty"`
	QA          map[string]any `json:"qa,omitempty"`
	Version     string         `json:"version"`
}
