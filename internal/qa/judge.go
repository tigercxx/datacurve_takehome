package qa

type JudgeResult struct {
	Model         string             `json:"model"`
	RubricVersion string             `json:"rubric_version"`
	Scores        map[string]float64 `json:"scores"`
	Overall       float64            `json:"overall"`
	Comments      string             `json:"comments"`
}

func Judge(traceSummary string, model string) *JudgeResult {
	scores := map[string]float64{
		"problem_understanding":    4,
		"investigation_strategy":   4,
		"decision_rationale":       4,
		"use_of_evidence":          4,
		"reproducibility":          4,
		"safety_privacy_awareness": 4,
	}
	return &JudgeResult{Model: model, RubricVersion: "v1", Scores: scores, Overall: 4.0, Comments: "stub"}
}
