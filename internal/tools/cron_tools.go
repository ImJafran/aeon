package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ImJafran/aeon/internal/scheduler"
)

// CronManageTool allows the LLM to manage scheduled jobs.
type CronManageTool struct {
	sched *scheduler.Scheduler
}

func NewCronManage(sched *scheduler.Scheduler) *CronManageTool {
	return &CronManageTool{sched: sched}
}

func (t *CronManageTool) Name() string { return "cron_manage" }
func (t *CronManageTool) Description() string {
	return "Set reminders and schedule tasks. Use this when a user says 'remind me', 'in X minutes', 'at X o'clock', or wants something recurring."
}
func (t *CronManageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["create", "list", "pause", "resume", "delete", "get"],
				"description": "The action to perform"
			},
			"id": {
				"type": "integer",
				"description": "Job ID (for pause, resume, delete, get)"
			},
			"name": {
				"type": "string",
				"description": "Short name for the reminder or task (e.g. 'iftar_party', 'standup_meeting')"
			},
			"schedule": {
				"type": "string",
				"description": "When to fire. Examples: 'in 1m', 'in 10m', 'in 2h' (one-time after delay), 'at 16:50', 'at 4:50pm' (one-time at clock time), 'every 5m', 'every 1h', 'daily', 'hourly' (recurring)"
			},
			"command": {
				"type": "string",
				"description": "The reminder text or message to send when it fires (e.g. 'Team standup meeting', 'Take medication')"
			},
			"skill_name": {
				"type": "string",
				"description": "Skill to run instead of a reminder message (optional, advanced)"
			},
			"params": {
				"type": "string",
				"description": "JSON params for the skill (optional, default: '{}')"
			}
		},
		"required": ["action"]
	}`)
}

type cronManageParams struct {
	Action    string `json:"action"`
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	SkillName string `json:"skill_name"`
	Command   string `json:"command"`
	Params    string `json:"params"`
}

func (t *CronManageTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p cronManageParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	switch p.Action {
	case "create":
		return t.create(p)
	case "list":
		return t.list()
	case "pause":
		return t.pause(p.ID)
	case "resume":
		return t.resume(p.ID)
	case "delete":
		return t.delete(p.ID)
	case "get":
		return t.get(p.ID)
	default:
		return ToolResult{ForLLM: fmt.Sprintf("Unknown action: %s. Use: create, list, pause, resume, delete, get.", p.Action)}, nil
	}
}

func (t *CronManageTool) create(p cronManageParams) (ToolResult, error) {
	if p.Name == "" || p.Schedule == "" {
		return ToolResult{ForLLM: "Error: name and schedule are required for create"}, nil
	}
	if p.SkillName == "" && p.Command == "" {
		return ToolResult{ForLLM: "Error: either skill_name or command is required"}, nil
	}
	if p.Params == "" {
		p.Params = "{}"
	}

	id, err := t.sched.Create(p.Name, p.Schedule, p.SkillName, p.Command, p.Params)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error creating job: %v", err)}, nil
	}

	label := "Scheduled"
	if scheduler.IsOneShot(p.Schedule) {
		label = "Reminder set"
	}
	return ToolResult{
		ForLLM:  fmt.Sprintf("Created (id=%d, name=%s, schedule=%s, command=%s)", id, p.Name, p.Schedule, p.Command),
		ForUser: fmt.Sprintf("%s: %s (%s)", label, p.Name, p.Schedule),
	}, nil
}

func (t *CronManageTool) list() (ToolResult, error) {
	jobs, err := t.sched.List(false)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error listing jobs: %v", err)}, nil
	}

	if len(jobs) == 0 {
		return ToolResult{ForLLM: "No scheduled jobs."}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Scheduled jobs (%d):\n", len(jobs)))
	for _, j := range jobs {
		status := "enabled"
		if !j.Enabled {
			status = "PAUSED"
		}
		lastRun := "never"
		if j.LastRun != nil {
			lastRun = j.LastRun.Format("2006-01-02 15:04")
		}
		b.WriteString(fmt.Sprintf("\n[%d] %s — %s [%s]", j.ID, j.Name, j.Schedule, status))
		b.WriteString(fmt.Sprintf("\n    last: %s | next: %s | fails: %d", lastRun, j.NextRun.Format("2006-01-02 15:04"), j.FailCount))
		if j.SkillName != "" {
			b.WriteString(fmt.Sprintf("\n    skill: %s", j.SkillName))
		}
		if j.Command != "" {
			b.WriteString(fmt.Sprintf("\n    command: %s", j.Command))
		}
	}

	return ToolResult{ForLLM: b.String()}, nil
}

func (t *CronManageTool) pause(id int64) (ToolResult, error) {
	if id <= 0 {
		return ToolResult{ForLLM: "Error: job id is required"}, nil
	}
	if err := t.sched.Pause(id); err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error pausing job: %v", err)}, nil
	}
	return ToolResult{ForLLM: fmt.Sprintf("Job %d paused.", id)}, nil
}

func (t *CronManageTool) resume(id int64) (ToolResult, error) {
	if id <= 0 {
		return ToolResult{ForLLM: "Error: job id is required"}, nil
	}
	if err := t.sched.Resume(id); err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error resuming job: %v", err)}, nil
	}
	return ToolResult{ForLLM: fmt.Sprintf("Job %d resumed.", id)}, nil
}

func (t *CronManageTool) delete(id int64) (ToolResult, error) {
	if id <= 0 {
		return ToolResult{ForLLM: "Error: job id is required"}, nil
	}
	if err := t.sched.Delete(id); err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error deleting job: %v", err)}, nil
	}
	return ToolResult{ForLLM: fmt.Sprintf("Job %d deleted.", id)}, nil
}

func (t *CronManageTool) get(id int64) (ToolResult, error) {
	if id <= 0 {
		return ToolResult{ForLLM: "Error: job id is required"}, nil
	}
	job, err := t.sched.Get(id)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error getting job: %v", err)}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Job #%d: %s\n", job.ID, job.Name))
	b.WriteString(fmt.Sprintf("Schedule: %s\n", job.Schedule))
	b.WriteString(fmt.Sprintf("Enabled: %v\n", job.Enabled))
	b.WriteString(fmt.Sprintf("Failures: %d\n", job.FailCount))
	if job.LastRun != nil {
		b.WriteString(fmt.Sprintf("Last run: %s\n", job.LastRun.Format(time.RFC3339)))
	}
	b.WriteString(fmt.Sprintf("Next run: %s\n", job.NextRun.Format(time.RFC3339)))
	if job.SkillName != "" {
		b.WriteString(fmt.Sprintf("Skill: %s\n", job.SkillName))
		b.WriteString(fmt.Sprintf("Params: %s\n", job.Params))
	}
	if job.Command != "" {
		b.WriteString(fmt.Sprintf("Command: %s\n", job.Command))
	}

	return ToolResult{ForLLM: b.String()}, nil
}
