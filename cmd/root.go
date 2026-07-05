// Copyright 2026
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package cmd implements the zarfs command-line interface.
package cmd

import (
	"github.com/spf13/cobra"
)

// Version information set via ldflags at build time.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "zarfs",
	Short: "Mount RISC OS archive files as filesystems",
	Long: `zarfs mounts RISC OS archive files (Spark, ArcFS, PackDir, Squash, CFS)
as filesystems. ArcFS archives support read-write access; other formats are
mounted read-only. Browse and modify their contents using standard file tools.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
