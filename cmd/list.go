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
	"bufio"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudygreybeard/zarfs/internal/archive"
	"github.com/cloudygreybeard/zarfs/internal/archive/arcfs"
	"github.com/cloudygreybeard/zarfs/internal/archive/cfs"
	"github.com/cloudygreybeard/zarfs/internal/archive/packdir"
	"github.com/cloudygreybeard/zarfs/internal/archive/spark"
	"github.com/cloudygreybeard/zarfs/internal/archive/squash"
	archivetar "github.com/cloudygreybeard/zarfs/internal/archive/tar"
	"github.com/cloudygreybeard/zarfs/internal/archive/targz"
	"github.com/cloudygreybeard/zarfs/internal/riscos"
)

var verbose bool

var listCmd = &cobra.Command{
	Use:   "list ARCHIVE",
	Short: "List contents of an archive",
	Long: `List files and directories within an archive file.

Supported formats: Tar, TarGz, Spark, ArcFS, PackDir, Squash, CFS.
Format is auto-detected from the file header.`,
	Args: cobra.ExactArgs(1),
	RunE: runList,
}

func init() {
	listCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show load/exec/attr/filetype/timestamp")
	listCmd.Flags().StringVarP(&listPassword, "password", "p", "", "archive password for garbled Spark/ArcFS archives")
	listCmd.Flags().StringVar(&listFormatFlag, "format", "", "archive format (tar, targz, arcfs, spark, packdir, squash, cfs); auto-detected if omitted")
	rootCmd.AddCommand(listCmd)
}

var (
	listPassword   string
	listFormatFlag string
)

func resolveListPassword(cmd *cobra.Command) ([]byte, error) {
	if v := os.Getenv(passwordEnvVar); v != "" {
		return []byte(v), nil
	}
	if !cmd.Flags().Changed("password") {
		return nil, nil
	}
	if listPassword == "" || listPassword == "-" {
		fmt.Fprintf(os.Stderr, "Password: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			return []byte(scanner.Text()), nil
		}
		return nil, fmt.Errorf("reading password from stdin: %w", scanner.Err())
	}
	fmt.Fprintf(os.Stderr, "zarfs: warning: command-line passwords are visible in process listings; prefer ZARFS_PASSWORD env var\n")
	return []byte(listPassword), nil
}

func runList(cmd *cobra.Command, args []string) error {
	path := args[0]

	var format archive.Format
	var err error
	if listFormatFlag != "" {
		format, err = archive.ParseFormat(listFormatFlag)
		if err != nil {
			return err
		}
	} else {
		format, err = archive.Detect(path)
		if err != nil {
			return fmt.Errorf("detecting format: %w", err)
		}
		if format == archive.FormatUnknown {
			return fmt.Errorf("unrecognised archive format (use --format to specify): %s", path)
		}
	}

	fmt.Fprintf(os.Stderr, "Format: %s\n", format)

	passwd, err := resolveListPassword(cmd)
	if err != nil {
		return err
	}

	arc, err := openForList(path, format, passwd)
	if err != nil {
		return err
	}
	defer func() { _ = arc.Close() }()

	printEntries(arc.Entries(), "", verbose)
	return nil
}

func printEntries(entries []*archive.Entry, prefix string, verb bool) {
	for _, e := range entries {
		path := prefix + e.Name
		if e.IsDir {
			path += "/"
		}

		if verb {
			ft := riscos.FileType(e.Load)
			ftStr := "---"
			if ft >= 0 {
				ftStr = fmt.Sprintf("%03x", ft)
			}
			fmt.Printf("%8d  %s  %s  %s\n", e.OrigLen, ftStr, e.FileTime.Format("2006-01-02 15:04:05"), path)
		} else {
			fmt.Println(path)
		}

		if e.IsDir && len(e.Children) > 0 {
			printEntries(e.Children, prefix+e.Name+"/", verb)
		}
	}
}

func openForList(path string, format archive.Format, passwd []byte) (archive.Archive, error) {
	switch format {
	case archive.FormatSpark:
		return spark.Open(path, passwd)
	case archive.FormatArcFS:
		return arcfs.Open(path, passwd)
	case archive.FormatPackDir:
		return packdir.Open(path)
	case archive.FormatSquash:
		return squash.Open(path)
	case archive.FormatCFS:
		return cfs.Open(path)
	case archive.FormatTar:
		return archivetar.Open(path)
	case archive.FormatTarGz:
		return targz.Open(path)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}
