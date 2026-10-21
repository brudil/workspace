package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRgArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantOpts rgOptions
		wantRest []string
		wantErr  string
	}{
		{
			name:     "no ws flags",
			args:     []string{"needle", "-t", "go"},
			wantRest: []string{"needle", "-t", "go"},
		},
		{
			name:     "repo repeated and comma separated",
			args:     []string{"--repo", "fe,api", "--repo=infra", "needle"},
			wantOpts: rgOptions{repos: []string{"fe", "api", "infra"}},
			wantRest: []string{"needle"},
		},
		{
			name:     "ground then rg flags",
			args:     []string{"--ground", "-i", "needle"},
			wantOpts: rgOptions{ground: true},
			wantRest: []string{"-i", "needle"},
		},
		{
			name:     "all",
			args:     []string{"--all", "needle"},
			wantOpts: rgOptions{all: true},
			wantRest: []string{"needle"},
		},
		{
			name:     "roots with nothing else",
			args:     []string{"--roots"},
			wantOpts: rgOptions{roots: true},
			wantRest: nil,
		},
		{
			name:     "double dash ends ws flags and is dropped",
			args:     []string{"--repo", "fe", "--", "-e", "-needle"},
			wantOpts: rgOptions{repos: []string{"fe"}},
			wantRest: []string{"-e", "-needle"},
		},
		{
			name:     "rg long flag that looks like ours is passed through",
			args:     []string{"--glob", "*.go", "needle"},
			wantRest: []string{"--glob", "*.go", "needle"},
		},
		{
			name:     "rg flags stop ws parsing even if ws flags follow",
			args:     []string{"-i", "--ground", "needle"},
			wantRest: []string{"-i", "--ground", "needle"},
		},
		{
			name:     "help",
			args:     []string{"--help"},
			wantOpts: rgOptions{help: true},
		},
		{
			name:    "ground and all conflict",
			args:    []string{"--ground", "--all", "needle"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "repo without value",
			args:    []string{"--repo"},
			wantErr: "requires a value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, rest, err := parseRgArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(opts, tt.wantOpts) {
				t.Errorf("opts = %+v, want %+v", opts, tt.wantOpts)
			}
			if !reflect.DeepEqual(rest, tt.wantRest) {
				t.Errorf("rest = %q, want %q", rest, tt.wantRest)
			}
		})
	}
}

func TestRewriteGlobs(t *testing.T) {
	prefixes := []string{"repos/fe/.ground", "repos/api/.ground"}
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "basename glob untouched",
			args: []string{"-g", "*.ts", "needle"},
			want: []string{"-g", "*.ts", "needle"},
		},
		{
			name: "negated basename glob untouched",
			args: []string{"-g", "!*.snap", "needle"},
			want: []string{"-g", "!*.snap", "needle"},
		},
		{
			name: "slash glob expanded per root",
			args: []string{"-g", "src/*.ts", "needle"},
			want: []string{"-g", "repos/fe/.ground/src/*.ts", "-g", "repos/api/.ground/src/*.ts", "needle"},
		},
		{
			name: "negated slash glob keeps negation",
			args: []string{"-g", "!src/**", "needle"},
			want: []string{"-g", "!repos/fe/.ground/src/**", "-g", "!repos/api/.ground/src/**", "needle"},
		},
		{
			name: "leading slash anchor is absorbed",
			args: []string{"--glob", "/src/*.ts", "needle"},
			want: []string{"--glob", "repos/fe/.ground/src/*.ts", "--glob", "repos/api/.ground/src/*.ts", "needle"},
		},
		{
			name: "doublestar prefix preserved",
			args: []string{"-g", "**/src/*.ts"},
			want: []string{"-g", "repos/fe/.ground/**/src/*.ts", "-g", "repos/api/.ground/**/src/*.ts"},
		},
		{
			name: "attached short form",
			args: []string{"-gsrc/*.ts", "needle"},
			want: []string{"-grepos/fe/.ground/src/*.ts", "-grepos/api/.ground/src/*.ts", "needle"},
		},
		{
			name: "equals long forms",
			args: []string{"--glob=src/*.ts", "--iglob=LIB/*.TS"},
			want: []string{
				"--glob=repos/fe/.ground/src/*.ts", "--glob=repos/api/.ground/src/*.ts",
				"--iglob=repos/fe/.ground/LIB/*.TS", "--iglob=repos/api/.ground/LIB/*.TS",
			},
		},
		{
			name: "iglob separate value",
			args: []string{"--iglob", "SRC/*", "x"},
			want: []string{"--iglob", "repos/fe/.ground/SRC/*", "--iglob", "repos/api/.ground/SRC/*", "x"},
		},
		{
			name: "nothing rewritten after double dash",
			args: []string{"-g", "src/*", "--", "-g", "src/*"},
			want: []string{"-g", "repos/fe/.ground/src/*", "-g", "repos/api/.ground/src/*", "--", "-g", "src/*"},
		},
		{
			name: "dangling -g left alone",
			args: []string{"needle", "-g"},
			want: []string{"needle", "-g"},
		},
		{
			name: "other flags untouched",
			args: []string{"-i", "-t", "go", "-C", "2", "--json", "needle"},
			want: []string{"-i", "-t", "go", "-C", "2", "--json", "needle"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteGlobs(tt.args, prefixes)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rewriteGlobs(%q)\n got %q\nwant %q", tt.args, got, tt.want)
			}
		})
	}
}
