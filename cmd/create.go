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

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	arcfsarchive "github.com/cloudygreybeard/zarfs/internal/archive/arcfs"
	archivetar "github.com/cloudygreybeard/zarfs/internal/archive/tar"
	"github.com/cloudygreybeard/zarfs/internal/archive/targz"
)

var createCmd = &cobra.Command{
	Use:   "create FORMAT PATH",
	Short: "Create an empty archive file",
	Long: `Create a new, empty archive file in the specified format.

The archive can then be mounted read-write with zarfs mount.

Supported formats: tar, targz, arcfs.`,
	Args: cobra.ExactArgs(2),
	RunE: runCreate,
}

func init() {
	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	path := args[1]

	format, err := archive.ParseFormat(args[0])
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	switch format {
	case archive.FormatTar:
		return createEmptyTar(path)
	case archive.FormatTarGz:
		return createEmptyTarGz(path)
	case archive.FormatArcFS:
		return createEmptyArcFS(path)
	default:
		return fmt.Errorf("format %q does not support creation", args[0])
	}
}

func createEmptyTar(path string) error {
	return archivetar.CreateEmpty(path)
}

func createEmptyTarGz(path string) error {
	return targz.CreateEmpty(path)
}

func createEmptyArcFS(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	_ = f.Close()

	arc, err := arcfsarchive.OpenRW(path, nil)
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("initialising arcfs: %w", err)
	}
	if err := arc.Flush(); err != nil {
		_ = arc.Close()
		_ = os.Remove(path)
		return fmt.Errorf("writing arcfs: %w", err)
	}
	return arc.Close()
}
