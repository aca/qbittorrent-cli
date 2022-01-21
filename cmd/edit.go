package cmd

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func RunEdit() *cobra.Command {
	var command = &cobra.Command{
		Use:   "edit",
		Short: "edit fast resume data",
		Long:  "edit torrent fast resume data. Make sure to backup data before.",
	}

	var (
		dry     bool
		verbose bool
		dir     string
		pattern string
		replace string
	)

	command.Flags().BoolVar(&dry, "dry-run", false, "Dry run, don't write changes")
	command.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	command.Flags().StringVar(&dir, "dir", "", "Dir with fast-resume files")
	command.Flags().StringVar(&pattern, "pattern", "", "Pattern to change")
	command.Flags().StringVar(&replace, "replace", "", "Text to replace pattern with")

	command.Run = func(cmd *cobra.Command, args []string) {
		if dir == "" {
			log.Fatal("must have dir\n")
		}

		matchedFiles := 0

		err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !!info.IsDir() {
				return nil //
			}

			if pattern == "" {
				return errors.New("must have pattern")
			} else if replace == "" {
				return errors.New("must have replace")
			}

			matched, err := filepath.Match("*.fastresume", info.Name())
			if err != nil {
				log.Fatalf("error matching files: %v", err)
			}

			if matched {
				read, err := ioutil.ReadFile(path)
				if err != nil {
					log.Fatalf("error reading file: %v - %v", path, err)
				}

				if verbose {
					fmt.Printf("Found: %v\n", path)
				}

				matchedFiles++

				if !dry {
					newContents := strings.Replace(string(read), pattern, replace, -1)

					err = ioutil.WriteFile(path, []byte(newContents), 0)
					if err != nil {
						log.Fatalf("error writing file: %v - %v", path, err)
					}
				}

				if verbose {
					fmt.Printf("Replaced: '%v' with '%v' for %v\n", pattern, replace, path)
				}
			}

			return nil
		})
		if err != nil {
			log.Fatalf("error reading files: %v", err)
		}

		fmt.Printf("Found, matched and replaced in '%d' files\n", matchedFiles)
	}

	return command
}