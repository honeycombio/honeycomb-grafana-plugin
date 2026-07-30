//go:build mage

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/build"
	"github.com/grafana/grafana-plugin-sdk-go/build/buildinfo"
	"github.com/magefile/mage/mg"
)

var Default = Build.Linux

type Build mg.Namespace

var b = build.Build{}

// The SDK stamps every binary with time.Now() via -ldflags, so two builds of the
// same commit produce different binaries and therefore a different release zip
// checksum. That breaks a promise we actually make: the release notes publish a
// SHA1 for users to verify, and RELEASING.md recommends re-running the Release
// workflow against an existing tag to recover from a failure — doing so would
// have silently changed the checksum out from under anyone who had verified it.
//
// Pin the timestamp to the commit rather than the clock. Registered in init() so
// it covers every target, including build:all.
func init() {
	if err := build.SetBeforeBuildCallback(pinBuildInfo); err != nil {
		panic(fmt.Sprintf("register reproducible-build callback: %v", err))
	}
}

func pinBuildInfo(cfg build.Config) (build.Config, error) {
	epoch, err := sourceDateEpoch()
	if err != nil {
		return cfg, err
	}

	// Built through the SDK's own type and AppendFlags rather than a hand-written
	// JSON string, so this keeps working if the SDK changes the payload shape.
	info := buildinfo.Info{
		Time:     epoch * 1000, // the SDK stores milliseconds
		PluginID: jsonStringField("src/plugin.json", "id"),
		Version:  jsonStringField("package.json", "version"),
	}

	if cfg.CustomVars == nil {
		cfg.CustomVars = map[string]string{}
	}
	// The SDK applies CustomVars after its own computed flags, so writing the same
	// keys here overrides the wall-clock value.
	info.AppendFlags(cfg.CustomVars)

	return cfg, nil
}

// sourceDateEpoch returns the timestamp to build against: SOURCE_DATE_EPOCH if
// set (the reproducible-builds convention), otherwise HEAD's committer date.
func sourceDateEpoch() (int64, error) {
	if v := strings.TrimSpace(os.Getenv("SOURCE_DATE_EPOCH")); v != "" {
		epoch, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse SOURCE_DATE_EPOCH %q: %w", v, err)
		}
		return epoch, nil
	}

	out, err := exec.Command("git", "log", "-1", "--format=%ct").Output()
	if err != nil {
		return 0, fmt.Errorf("read commit timestamp (set SOURCE_DATE_EPOCH when building outside a git checkout): %w", err)
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

func jsonStringField(path, key string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	var value string
	if err := json.Unmarshal(fields[key], &value); err != nil {
		return ""
	}
	return value
}

func (Build) Linux() error {
	return b.Linux()
}

func (Build) LinuxARM64() error {
	return b.LinuxARM64()
}

func (Build) Darwin() error {
	return b.Darwin()
}

func (Build) Windows() error {
	return b.Windows()
}

func (Build) All() {
	build.BuildAll()
}

func Test() error {
	return build.Test()
}

func Coverage() error {
	return build.Coverage()
}

func Lint() error {
	return build.Lint()
}

func Clean() error {
	return build.Clean()
}
