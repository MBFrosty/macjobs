package brew

import "encoding/json"

type Service struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	File     string `json:"file"`
	ExitCode *int   `json:"exit_code"`
}

func Parse(b []byte) ([]Service, error) { var s []Service; err := json.Unmarshal(b, &s); return s, err }
