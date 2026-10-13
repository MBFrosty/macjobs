package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const prettyGap = 3

var prettyHeaders = []string{"ID", "KIND", "OWNER", "TAGS", "STATE", "PORTS", "HEALTH", "PURPOSE"}

type prettyStyles struct {
	header lipgloss.Style
	tag    lipgloss.Style
	warn   lipgloss.Style
	kind   map[string]lipgloss.Style
	health map[string]lipgloss.Style
	muted  lipgloss.Style
}

func writeHuman(w io.Writer, r model.Report, red redact.Redactor, pretty, color, findings bool) error {
	if pretty {
		return writePretty(w, r, red, color, findings)
	}
	return Table(w, r, red)
}

func writePretty(w io.Writer, r model.Report, red redact.Redactor, color, findings bool) error {
	rend := newPrettyRenderer(w, color)
	st := newPrettyStyles(rend, color)
	rows := make([][]string, 0, len(r.Items))
	for _, item := range r.Items {
		rows = append(rows, prettyRow(item, red, st, findings))
	}
	widths := make([]int, len(prettyHeaders))
	for i, h := range prettyHeaders {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := lipgloss.Width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	gap := strings.Repeat(" ", prettyGap)
	var b strings.Builder
	header := make([]string, len(prettyHeaders))
	for i, h := range prettyHeaders {
		header[i] = padRight(st.header.Render(h), widths[i])
	}
	b.WriteString(strings.Join(header, gap))
	b.WriteByte('\n')
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = padRight(cell, widths[i])
		}
		b.WriteString(strings.Join(cells, gap))
		b.WriteByte('\n')
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	for _, warning := range r.Warnings {
		line := "Warning: " + plainCell(warning, red)
		if _, err := fmt.Fprintln(w, st.warn.Render(line)); err != nil {
			return err
		}
	}
	return nil
}

func writeShowHeader(w io.Writer, item model.Item, red redact.Redactor, color bool) error {
	st := newPrettyStyles(newPrettyRenderer(w, color), color)
	kind := plainCell(item.Kind, red)
	id := plainCell(item.ID, red)
	state := plainCell(item.State, red)
	health := plainCell(item.Health, red)
	kindStyle := st.kindStyle(kind)
	healthStyle := st.healthStyle(health)
	line := strings.Join([]string{
		kindStyle.Render("●") + " " + id,
		kindStyle.Render(kind),
		st.muted.Render(state),
		healthStyle.Render("●") + " " + healthStyle.Render(health),
	}, strings.Repeat(" ", prettyGap))
	_, err := fmt.Fprintln(w, line)
	return err
}

func prettyRow(item model.Item, red redact.Redactor, st prettyStyles, findings bool) []string {
	kind := plainCell(item.Kind, red)
	id := plainCell(item.ID, red)
	owner := plainCell(item.Owner, red)
	if owner == "" {
		owner = "-"
	}
	tags := "-"
	if len(item.Tags) > 0 {
		parts := make([]string, 0, len(item.Tags))
		for _, tag := range item.Tags {
			parts = append(parts, st.tag.Render("("+plainCell(tag, red)+")"))
		}
		tags = strings.Join(parts, " ")
	}
	state := plainCell(item.State, red)
	if state == "" {
		state = "-"
	}
	ports := plainCell(FormatPorts(item.Ports), red)
	if strings.Contains(ports, "!") {
		ports = st.healthStyle("warn").Render(ports)
	}
	health := plainCell(item.Health, red)
	if health == "" {
		health = "unknown"
	}
	purpose := plainCell(item.Purpose, red)
	if findings && len(item.Notes) > 0 {
		notes := st.healthStyle(health).Render(plainCell(strings.Join(item.Notes, "; "), red))
		if purpose == "" {
			purpose = notes
		} else {
			purpose = purpose + "  " + notes
		}
	}
	if purpose == "" {
		purpose = "-"
	}
	kindStyle := st.kindStyle(kind)
	healthStyle := st.healthStyle(health)
	idCell := kindStyle.Render("●") + " " + id
	healthCell := healthStyle.Render("●") + " " + healthStyle.Render(health)
	return []string{idCell, kindStyle.Render(kind), st.muted.Render(owner), tags, st.muted.Render(state), ports, healthCell, purpose}
}

func newPrettyRenderer(w io.Writer, color bool) *lipgloss.Renderer {
	rend := lipgloss.NewRenderer(w)
	if color {
		rend.SetColorProfile(termenv.TrueColor)
		if !writerIsTTY(w) {
			rend.SetHasDarkBackground(true)
		}
	} else {
		rend.SetColorProfile(termenv.Ascii)
	}
	return rend
}

func newPrettyStyles(r *lipgloss.Renderer, color bool) prettyStyles {
	ident := r.NewStyle()
	if !color {
		return prettyStyles{
			header: ident,
			tag:    ident,
			warn:   ident,
			muted:  ident,
			kind: map[string]lipgloss.Style{
				"launchd": ident, "system": ident, "brew": ident, "tailscale": ident, "process": ident, "cron": ident,
			},
			health: map[string]lipgloss.Style{
				"ok": ident, "warn": ident, "fail": ident, "unknown": ident,
			},
		}
	}
	launchd := r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4B5563", Dark: "#C8C8D0"})
	return prettyStyles{
		header: r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}).Faint(true),
		tag:    r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4B5563", Dark: "#A8A8B8"}),
		warn:   r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#E8C84A"}),
		muted:  r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4B5563", Dark: "#B0B0BC"}),
		kind: map[string]lipgloss.Style{
			"launchd":   launchd,
			"system":    r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}),
			"brew":      r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#E6B422"}),
			"tailscale": r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#5BA8DC"}),
			"process":   r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A888E0"}),
			"cron":      r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#BE185D", Dark: "#E090B0"}),
		},
		health: map[string]lipgloss.Style{
			"ok":      r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}),
			"warn":    r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#E8C84A"}),
			"fail":    r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}),
			"unknown": r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}),
		},
	}
}

func (st prettyStyles) kindStyle(kind string) lipgloss.Style {
	if s, ok := st.kind[kind]; ok {
		return s
	}
	return st.kind["launchd"]
}

func (st prettyStyles) healthStyle(health string) lipgloss.Style {
	if s, ok := st.health[health]; ok {
		return s
	}
	return st.health["unknown"]
}

func padRight(s string, width int) string {
	extra := width - lipgloss.Width(s)
	if extra <= 0 {
		return s
	}
	return s + strings.Repeat(" ", extra)
}

func plainCell(s string, red redact.Redactor) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(red.Text(s))
}
