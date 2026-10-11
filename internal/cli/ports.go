package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/MBFrosty/macjobs/internal/health"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
)

func FormatPorts(ports []model.PortBinding) string {
	observed := []string{}
	missing := []string{}
	seenObs := map[string]bool{}
	seenMiss := map[string]bool{}
	for _, b := range ports {
		label := health.FormatPort(b.Proto, b.Port)
		if b.Source == model.PortObserved || b.Listening {
			if !seenObs[label] {
				observed = append(observed, label)
				seenObs[label] = true
			}
			continue
		}
		if !seenMiss[label] && !seenObs[label] {
			missing = append(missing, "!"+label)
			seenMiss[label] = true
		}
	}
	out := append(observed, missing...)
	if len(out) == 0 {
		return "-"
	}
	if len(out) > 4 {
		return strings.Join(out[:4], ",") + ",+" + strconv.Itoa(len(out)-4)
	}
	return strings.Join(out, ",")
}

func itemHasPort(i model.Item, port int) bool {
	if port < 1 {
		return false
	}
	if i.Port == port {
		return true
	}
	for _, b := range i.Ports {
		if b.Port == port {
			return true
		}
	}
	return false
}

func writePorts(w io.Writer, result inventory.Result, unattributed bool, red redact.Redactor) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, e := fmt.Fprintln(tw, "PROTO\tPORT\tHOST\tPID\tCOMMAND\tJOB\tVIA\tFINDINGS"); e != nil {
		return e
	}
	for _, l := range result.Listeners {
		if unattributed && l.JobID != "" {
			continue
		}
		job, via := "-", "-"
		if l.JobID != "" {
			job = l.JobID
		}
		if l.Via != "" {
			via = l.Via
		}
		findings := []string{}
		if l.JobID == "" {
			findings = append(findings, "unattributed")
		}
		if len(l.ExpectedBy) > 0 {
			findings = append(findings, "expected by "+strings.Join(l.ExpectedBy, ", "))
		}
		note := "-"
		if len(findings) > 0 {
			note = strings.Join(findings, "; ")
		}
		values := []string{l.Proto, strconv.Itoa(l.Port), l.Host, fmt.Sprint(l.PID), l.Command, job, via, note}
		for n, v := range values {
			values[n] = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(red.Text(v))
		}
		if _, e := fmt.Fprintln(tw, strings.Join(values, "\t")); e != nil {
			return e
		}
	}
	if e := tw.Flush(); e != nil {
		return e
	}
	for _, warning := range result.Report.Warnings {
		if _, e := fmt.Fprintln(w, "Warning:", red.Text(warning)); e != nil {
			return e
		}
	}
	return nil
}

type portsReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	Listeners     []model.Listener `json:"listeners"`
	Warnings      []string         `json:"warnings"`
}
