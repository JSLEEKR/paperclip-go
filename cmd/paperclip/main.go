// Command paperclip is the CLI for paperclip-go — an AI company orchestration engine.
//
// Usage:
//
//	paperclip company create --name "Acme AI" --slug acme-ai
//	paperclip agent hire --company acme-ai --name "Claude" --role executor
//	paperclip task create --company acme-ai --title "Build feature X"
//	paperclip budget set --company acme-ai --scope agent --scope-id <id> --amount 5000
//	paperclip routine create --company acme-ai --title "Daily standup" --cron "0 9 * * *"
//	paperclip status --company acme-ai
//	paperclip version
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JSLEEKR/paperclip-go/pkg/agent"
	"github.com/JSLEEKR/paperclip-go/pkg/budget"
	"github.com/JSLEEKR/paperclip-go/pkg/company"
	"github.com/JSLEEKR/paperclip-go/pkg/goal"
	"github.com/JSLEEKR/paperclip-go/pkg/heartbeat"
	"github.com/JSLEEKR/paperclip-go/pkg/routine"
	"github.com/JSLEEKR/paperclip-go/pkg/store"
	"github.com/JSLEEKR/paperclip-go/pkg/task"
)

// Version is set at build time.
var Version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	dataDir := os.Getenv("PAPERCLIP_DATA_DIR")
	if dataDir == "" {
		dataDir = ".paperclip"
	}

	s := store.New(dataDir)
	companySvc := company.NewService(s)
	agentSvc := agent.NewService(s)
	taskSvc := task.NewService(s)
	budgetSvc := budget.NewService(s)
	routineSvc := routine.NewService(s)
	goalSvc := goal.NewService(s)

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "version":
		fmt.Printf("paperclip-go v%s\n", Version)

	case "company":
		handleCompany(companySvc, args)

	case "agent":
		handleAgent(agentSvc, args)

	case "task":
		handleTask(taskSvc, args)

	case "budget":
		handleBudget(budgetSvc, args)

	case "routine":
		handleRoutine(routineSvc, args)

	case "goal":
		handleGoal(goalSvc, args)

	case "status":
		handleStatus(companySvc, agentSvc, taskSvc, budgetSvc, routineSvc, goalSvc, s)

	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`paperclip-go — AI company orchestration engine

Usage: paperclip <command> [options]

Commands:
  company    Manage AI companies
  agent      Manage AI agents (hire, fire, pause, resume)
  task       Manage tasks (create, assign, checkout, complete)
  budget     Set and manage budget policies
  routine    Create scheduled routines
  goal       Manage company goals
  status     Show company status overview
  version    Print version

Environment:
  PAPERCLIP_DATA_DIR  Data directory (default: .paperclip)`)
}

func handleCompany(svc *company.Service, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: paperclip company <create|list|get|delete>")
		return
	}

	switch args[0] {
	case "create":
		flags := parseFlags(args[1:])
		name := flags["name"]
		slug := flags["slug"]
		if name == "" || slug == "" {
			fmt.Println("Usage: paperclip company create --name <name> --slug <slug>")
			return
		}
		c, err := svc.Create(company.Company{
			ID:   generateID("co"),
			Name: name,
			Slug: slug,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(c)

	case "list":
		companies := svc.List()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSLUG\tSTATUS")
		for _, c := range companies {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.ID, c.Name, c.Slug, c.Status)
		}
		w.Flush()

	case "get":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip company get <id>")
			return
		}
		c, err := svc.Get(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(c)

	case "delete":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip company delete <id>")
			return
		}
		if err := svc.Delete(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("deleted")

	default:
		fmt.Printf("Usage: paperclip company <create|list|get|delete>\n")
	}
}

func handleAgent(svc *agent.Service, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: paperclip agent <hire|fire|pause|resume|list|get>")
		return
	}

	switch args[0] {
	case "hire":
		flags := parseFlags(args[1:])
		if flags["name"] == "" || flags["company"] == "" {
			fmt.Println("Usage: paperclip agent hire --company <id> --name <name> [--role <role>] [--adapter <type>]")
			return
		}
		a, err := svc.Hire(agent.Agent{
			ID:          generateID("ag"),
			CompanyID:   flags["company"],
			Name:        flags["name"],
			Role:        agent.Role(flags["role"]),
			Title:       flags["title"],
			AdapterType: flags["adapter"],
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(a)

	case "fire":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip agent fire <id>")
			return
		}
		if err := svc.Fire(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("fired")

	case "pause":
		flags := parseFlags(args[1:])
		if flags["id"] == "" {
			fmt.Println("Usage: paperclip agent pause --id <id> [--reason <reason>]")
			return
		}
		if err := svc.Pause(flags["id"], flags["reason"]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("paused")

	case "resume":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip agent resume <id>")
			return
		}
		if err := svc.Resume(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("resumed")

	case "list":
		flags := parseFlags(args[1:])
		var agents []agent.Agent
		if flags["company"] != "" {
			agents = svc.ListByCompany(flags["company"])
		} else {
			agents = svc.List()
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tROLE\tSTATUS\tCOMPANY")
		for _, a := range agents {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.ID, a.Name, a.Role, a.Status, a.CompanyID)
		}
		w.Flush()

	case "get":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip agent get <id>")
			return
		}
		a, err := svc.Get(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(a)

	default:
		fmt.Printf("Usage: paperclip agent <hire|fire|pause|resume|list|get>\n")
	}
}

func handleTask(svc *task.Service, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: paperclip task <create|list|assign|checkout|complete|transition>")
		return
	}

	switch args[0] {
	case "create":
		flags := parseFlags(args[1:])
		if flags["company"] == "" || flags["title"] == "" {
			fmt.Println("Usage: paperclip task create --company <id> --title <title> [--priority <priority>]")
			return
		}
		t, err := svc.Create(task.Task{
			ID:        generateID("task"),
			CompanyID: flags["company"],
			Title:     flags["title"],
			Priority:  task.Priority(flags["priority"]),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(t)

	case "list":
		flags := parseFlags(args[1:])
		var tasks []task.Task
		if flags["company"] != "" {
			tasks = svc.ListByCompany(flags["company"])
		} else if flags["assignee"] != "" {
			tasks = svc.ListByAssignee(flags["assignee"])
		} else {
			tasks = svc.List()
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tPRIORITY\tASSIGNEE")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Title, t.Status, t.Priority, t.AssigneeAgentID)
		}
		w.Flush()

	case "assign":
		flags := parseFlags(args[1:])
		if flags["id"] == "" || flags["agent"] == "" {
			fmt.Println("Usage: paperclip task assign --id <task-id> --agent <agent-id>")
			return
		}
		if err := svc.Assign(flags["id"], flags["agent"]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("assigned")

	case "checkout":
		flags := parseFlags(args[1:])
		if flags["id"] == "" || flags["agent"] == "" || flags["run"] == "" {
			fmt.Println("Usage: paperclip task checkout --id <task-id> --agent <agent-id> --run <run-id>")
			return
		}
		if err := svc.Checkout(flags["id"], flags["agent"], flags["run"]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("checked out")

	case "complete":
		flags := parseFlags(args[1:])
		if flags["id"] == "" {
			fmt.Println("Usage: paperclip task complete --id <task-id> [--run <run-id>]")
			return
		}
		if err := svc.Complete(flags["id"], flags["run"]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("completed")

	default:
		fmt.Printf("Usage: paperclip task <create|list|assign|checkout|complete>\n")
	}
}

func handleBudget(svc *budget.Service, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: paperclip budget <set|list|incidents>")
		return
	}

	switch args[0] {
	case "set":
		flags := parseFlags(args[1:])
		if flags["company"] == "" || flags["scope"] == "" || flags["scope-id"] == "" || flags["amount"] == "" {
			fmt.Println("Usage: paperclip budget set --company <id> --scope <company|agent|project> --scope-id <id> --amount <cents>")
			return
		}
		var amount int64
		fmt.Sscanf(flags["amount"], "%d", &amount)
		p, err := svc.CreatePolicy(budget.Policy{
			ID:          generateID("bp"),
			CompanyID:   flags["company"],
			Scope:       budget.Scope(flags["scope"]),
			ScopeID:     flags["scope-id"],
			AmountCents: amount,
			WarnPercent: 0.8,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(p)

	case "list":
		flags := parseFlags(args[1:])
		if flags["company"] == "" {
			fmt.Println("Usage: paperclip budget list --company <id>")
			return
		}
		policies := svc.ListPolicies(flags["company"])
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSCOPE\tSCOPE_ID\tAMOUNT\tWARN%")
		for _, p := range policies {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%.0f%%\n", p.ID, p.Scope, p.ScopeID, p.AmountCents, p.WarnPercent*100)
		}
		w.Flush()

	case "incidents":
		flags := parseFlags(args[1:])
		if flags["company"] == "" {
			fmt.Println("Usage: paperclip budget incidents --company <id>")
			return
		}
		incidents := svc.ListIncidents(flags["company"])
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSEVERITY\tSTATUS\tOBSERVED\tLIMIT\tMESSAGE")
		for _, i := range incidents {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n", i.ID, i.Severity, i.Status, i.Observed, i.Limit, i.Message)
		}
		w.Flush()

	default:
		fmt.Printf("Usage: paperclip budget <set|list|incidents>\n")
	}
}

func handleRoutine(svc *routine.Service, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: paperclip routine <create|list|enable|disable>")
		return
	}

	switch args[0] {
	case "create":
		flags := parseFlags(args[1:])
		if flags["company"] == "" || flags["title"] == "" || flags["agent"] == "" {
			fmt.Println("Usage: paperclip routine create --company <id> --title <title> --agent <agent-id> [--cron <expr>]")
			return
		}
		var triggers []routine.Trigger
		if flags["cron"] != "" {
			triggers = append(triggers, routine.Trigger{
				ID:             generateID("trig"),
				Kind:           routine.TriggerSchedule,
				CronExpression: flags["cron"],
				Timezone:       flags["timezone"],
			})
		}
		r, err := svc.Create(routine.Routine{
			ID:              generateID("routine"),
			CompanyID:       flags["company"],
			Title:           flags["title"],
			AssigneeAgentID: flags["agent"],
			Triggers:        triggers,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(r)

	case "list":
		flags := parseFlags(args[1:])
		var routines []routine.Routine
		if flags["company"] != "" {
			routines = svc.ListByCompany(flags["company"])
		} else {
			routines = svc.List()
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tTITLE\tAGENT\tENABLED\tTRIGGERS")
		for _, r := range routines {
			fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%d\n", r.ID, r.Title, r.AssigneeAgentID, r.Enabled, len(r.Triggers))
		}
		w.Flush()

	case "enable":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip routine enable <id>")
			return
		}
		if err := svc.Enable(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("enabled")

	case "disable":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip routine disable <id>")
			return
		}
		if err := svc.Disable(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("disabled")

	default:
		fmt.Printf("Usage: paperclip routine <create|list|enable|disable>\n")
	}
}

func handleGoal(svc *goal.Service, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: paperclip goal <create|list|complete|abandon>")
		return
	}

	switch args[0] {
	case "create":
		flags := parseFlags(args[1:])
		if flags["company"] == "" || flags["title"] == "" {
			fmt.Println("Usage: paperclip goal create --company <id> --title <title> [--description <desc>]")
			return
		}
		g, err := svc.Create(goal.Goal{
			ID:          generateID("goal"),
			CompanyID:   flags["company"],
			Title:       flags["title"],
			Description: flags["description"],
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printJSON(g)

	case "list":
		flags := parseFlags(args[1:])
		var goals []goal.Goal
		if flags["company"] != "" {
			goals = svc.ListByCompany(flags["company"])
		} else {
			goals = svc.List()
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tPROGRESS")
		for _, g := range goals {
			fmt.Fprintf(w, "%s\t%s\t%s\t%.0f%%\n", g.ID, g.Title, g.Status, g.Progress*100)
		}
		w.Flush()

	case "complete":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip goal complete <id>")
			return
		}
		if err := svc.Complete(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("completed")

	case "abandon":
		if len(args) < 2 {
			fmt.Println("Usage: paperclip goal abandon <id>")
			return
		}
		if err := svc.Abandon(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("abandoned")

	default:
		fmt.Printf("Usage: paperclip goal <create|list|complete|abandon>\n")
	}
}

func handleStatus(
	companySvc *company.Service,
	agentSvc *agent.Service,
	taskSvc *task.Service,
	budgetSvc *budget.Service,
	routineSvc *routine.Service,
	goalSvc *goal.Service,
	s *store.Store,
) {
	hbEng := heartbeat.NewEngine(s)

	fmt.Println("=== paperclip-go Status ===")
	fmt.Println()

	companies := companySvc.List()
	fmt.Printf("Companies: %d\n", len(companies))
	for _, c := range companies {
		fmt.Printf("  - %s (%s) [%s]\n", c.Name, c.Slug, c.Status)

		agents := agentSvc.ListByCompany(c.ID)
		fmt.Printf("    Agents: %d\n", len(agents))
		for _, a := range agents {
			fmt.Printf("      - %s (%s) [%s]\n", a.Name, a.Role, a.Status)
		}

		tasks := taskSvc.ListByCompany(c.ID)
		fmt.Printf("    Tasks: %d\n", len(tasks))

		runs := hbEng.List()
		var runCount int
		for _, r := range runs {
			if r.CompanyID == c.ID {
				runCount++
			}
		}
		fmt.Printf("    Heartbeat Runs: %d\n", runCount)

		policies := budgetSvc.ListPolicies(c.ID)
		fmt.Printf("    Budget Policies: %d\n", len(policies))

		incidents := budgetSvc.ListOpenIncidents(c.ID)
		if len(incidents) > 0 {
			fmt.Printf("    Open Incidents: %d\n", len(incidents))
		}

		routines := routineSvc.ListByCompany(c.ID)
		fmt.Printf("    Routines: %d\n", len(routines))

		goals := goalSvc.ListByCompany(c.ID)
		fmt.Printf("    Goals: %d\n", len(goals))
	}

	if len(companies) == 0 {
		fmt.Println("  (no companies — run 'paperclip company create' to get started)")
	}
}

func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = args[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		}
	}
	return flags
}

func generateID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}
