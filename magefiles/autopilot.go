//go:build mage

package main

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/magefile/mage/mg"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/mcpserver"
)

const (
	// evalsDir is the eval suite of the plugin Describe it installs in the user's own
	// Claude Code, in the checkout: autopilot:evals copies it beside the plugin's skill.
	evalsDir = "internal/describeit/evals"

	// evalsBudget caps what one run of the suite may spend, at list price, in dollars,
	// unless budget= says otherwise.
	evalsBudget = "5"
)

// evalsFilled are the suite's files whose names describeit.EvalText fills in.
func evalsFilled() []string {
	return []string{".md", ".yaml", ".yml", ".json"}
}

// Autopilot groups the targets for Autopilot's Describe it.
type Autopilot mg.Namespace

// Evals runs the eval suite of the plugin Describe it installs in the user's own Claude
// Code (internal/describeit/evals) against this build's plugin: it writes the plugin to a
// temporary folder as the app writes it (its skill from this build's words), with the
// suite beside it, and runs Claude Code's plugin eval there, with the baseline arm left
// out and a cap on what it spends. The plugin's tools answer from the suite's mocks, so
// nothing reaches the app or its data. Run it by hand after the catalog or the skill's
// words change; it isn't part of the gate. The folder is kept, with the report.
//
//	magex autopilot:evals [runs=<n>] [case=<glob>] [model=<model>] [budget=<dollars>]
func (Autopilot) Evals() error {
	return hostRunner().runEvals()
}

// runEvals does Autopilot.Evals' work.
func (r runner) runEvals() error {
	program := agentProgram()

	if _, err := r.lookPath(program); err != nil {
		return fmt.Errorf("find Claude Code (%s) to run the evals: %w", program, err)
	}

	dir, err := os.MkdirTemp(r.tempDir(), "autopilot-evals-")
	if err != nil {
		return fmt.Errorf("make the evals' folder: %w", err)
	}

	root, err := r.writeEvalPlugin(dir)
	if err != nil {
		return err
	}

	r.say("Running the plugin's evals in " + root)

	return r.runV(program, r.evalArgs(root)...)
}

// agentProgram is the program Describe it talks with: Claude Code's.
func agentProgram() string {
	a, err := agent.Lookup(agent.Claude)
	if err != nil {
		return string(agent.Claude)
	}

	return a.Command
}

// writeEvalPlugin writes this build's plugin into dir, as the app writes it in its
// describe folder, with the checkout's eval suite in the plugin's own folder, and returns
// that folder.
func (r runner) writeEvalPlugin(dir string) (string, error) {
	market := filepath.Join(dir, "plugin")
	link := filepath.Join(dir, "bin", brand.Slug+"-mcp")

	if _, err := describeit.WritePlugin(market, describeit.Plugin(link, []string{mcpserver.Arg(dir)})); err != nil {
		return "", fmt.Errorf("write the plugin: %w", err)
	}

	root := filepath.Join(market, describeit.PluginName)

	if err := copyEvals(r.path(evalsDir), filepath.Join(root, "evals")); err != nil {
		return "", fmt.Errorf("copy the eval suite: %w", err)
	}

	return root, nil
}

// copyEvals copies the eval suite in src to dst, filling in the names it leaves for the
// build (describeit.EvalText), and leaves out the results of earlier runs.
func copyEvals(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		switch {
		case d.IsDir() && d.Name() == "results":
			return filepath.SkipDir
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dst, rel), 0o700)
		case !d.Type().IsRegular():
			return nil
		default:
			return copyEval(path, filepath.Join(dst, rel))
		}
	})
}

// copyEval copies the suite's file at path to dst, its names filled in when it is text.
func copyEval(path, dst string) error {
	data, err := os.ReadFile(path) //nolint:gosec // a file of the checkout's own suite
	if err != nil {
		return err
	}

	if slices.Contains(evalsFilled(), filepath.Ext(path)) {
		data = []byte(describeit.EvalText(string(data)))
	}

	return os.WriteFile(dst, data, 0o600) //nolint:gosec // under the evals' own temporary folder, as the suite lays it out
}

// evalArgs are the arguments of Claude Code's plugin eval of the plugin in root: trusted,
// as the checkout's own; its report kept local; the plugin's arm alone; and the runs,
// cases, model, and budget the target's arguments name.
func (r runner) evalArgs(root string) []string {
	args := []string{
		"plugin", "eval", root, "--trust-plugin", "--no-publish", "--ablation", "none",
		"--max-cost-usd", cmp.Or(r.mageArg("budget"), evalsBudget),
	}

	for _, flag := range []struct{ arg, flag string }{{"runs", "--runs"}, {"case", "--case"}, {"model", "--model"}} {
		if value := r.mageArg(flag.arg); value != "" {
			args = append(args, flag.flag, value)
		}
	}

	return args
}
