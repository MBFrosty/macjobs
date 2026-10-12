package model

import "time"

const SchemaVersion = 1

type CronEntry struct {
	Schedule string            `json:"schedule"`
	Command  string            `json:"command"`
	Env      map[string]string `json:"env"`
}

type Item struct {
	Command         string         `json:"command,omitempty"`
	Source          string         `json:"source,omitempty"`
	Line            int            `json:"line,omitempty"`
	RunAs           string         `json:"runAs,omitempty"`
	CronEntry       *CronEntry     `json:"-"`
	CronProgram     string         `json:"-"`
	CronMalformed   bool           `json:"-"`
	CronPercent     bool           `json:"-"`
	Tags            []string       `json:"tags,omitempty"`
	BrewName        string         `json:"-"`
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	Label           string         `json:"label,omitempty"`
	Domain          string         `json:"domain,omitempty"`
	Owner           string         `json:"owner"`
	Purpose         string         `json:"purpose,omitempty"`
	Managed         bool           `json:"managed"`
	CreatedByTool   bool           `json:"created_by_tool"`
	State           string         `json:"state"`
	PID             int            `json:"pid,omitempty"`
	Schedule        string         `json:"schedule,omitempty"`
	LastRun         *time.Time     `json:"lastRun,omitempty"`
	LastExit        *int           `json:"lastExit,omitempty"`
	Signal          string         `json:"signal,omitempty"`
	Runs            int            `json:"runs,omitempty"`
	Health          string         `json:"health"`
	Notes           []string       `json:"notes"`
	PlistPath       string         `json:"plistPath,omitempty"`
	Plist           map[string]any `json:"-"`
	Logs            []string       `json:"logs,omitempty"`
	Port            int            `json:"port,omitempty"`
	Ports           []PortBinding  `json:"ports,omitempty"`
	Target          string         `json:"target,omitempty"`
	Funnel          bool           `json:"funnel,omitempty"`
	ReadOnly        bool           `json:"readOnly"`
	Registered      bool           `json:"-"`
	Live            bool           `json:"-"`
	Observed        bool           `json:"-"`
	PlistMissing    bool           `json:"-"`
	PlistUnreadable bool           `json:"-"`
	KeepAlive       bool           `json:"-"`
	Disabled        bool           `json:"disabled,omitempty"`
	Interval        time.Duration  `json:"-"`
}
type PortBinding struct {
	Proto     string `json:"proto"`
	Port      int    `json:"port"`
	Host      string `json:"host,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Source    string `json:"source"`
	Listening bool   `json:"listening"`
	Via       string `json:"via,omitempty"`
}

const (
	PortDeclared  = "declared"
	PortSocket    = "socket"
	PortInferred  = "inferred"
	PortTailscale = "tailscale"
	PortObserved  = "observed"
)

type Listener struct {
	Proto      string   `json:"proto"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	PID        int      `json:"pid,omitempty"`
	Command    string   `json:"command,omitempty"`
	JobID      string   `json:"jobId,omitempty"`
	Via        string   `json:"via,omitempty"`
	ExpectedBy []string `json:"expectedBy"`
}

type Report struct {
	SchemaVersion int      `json:"schemaVersion"`
	Items         []Item   `json:"items"`
	Warnings      []string `json:"warnings"`
}

func NewReport() Report {
	return Report{SchemaVersion: SchemaVersion, Items: []Item{}, Warnings: []string{}}
}
