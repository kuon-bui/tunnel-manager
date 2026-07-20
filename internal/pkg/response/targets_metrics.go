package response

type TargetsMetrics struct {
	Targets []string      `json:"targets"`
	Labels  LabelsMetrics `json:"labels"`
}

type LabelsMetrics struct {
	Env         string `json:"env"`
	Domain      string `json:"domain"`
	MetricsPath string `json:"__metrics_path__"`
}
