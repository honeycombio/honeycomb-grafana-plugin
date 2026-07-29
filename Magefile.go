//go:build mage

package main

import (
	"github.com/grafana/grafana-plugin-sdk-go/build"
	"github.com/magefile/mage/mg"
)

var Default = Build.Linux

type Build mg.Namespace

var b = build.Build{}

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
