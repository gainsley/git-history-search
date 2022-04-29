package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strings"
)

var ci = flag.Bool("ci", false, "case-insensitive search")
var lookup = flag.String("lookup", "", "use lookup string instead of replace file")

func main() {
	flag.Parse()
	replaceFile := flag.Arg(0)

	replaceMap := make(map[string]string)
	if replaceFile == "" {
		if *lookup == "" {
			panic("search term or file not specified")
		}
		replaceMap[*lookup] = ""
	} else {
		contents, err := ioutil.ReadFile(replaceFile)
		if err != nil {
			panic(err.Error())
		}
		for _, line := range strings.Split(string(contents), "\n") {
			if line == "" {
				continue
			}
			parts := strings.Split(line, "==>")
			if len(parts) != 2 {
				continue
			}
			replaceMap[parts[0]] = parts[1]
		}
	}

	cmd := exec.Command("git", "log", "--all", "--full-history", "-p", "-U0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic(string(out) + "\n" + err.Error())
	}
	curFile := ""
	curCommit := ""
	matchingFiles := make(map[string]bool) // value is if file is deleted in current branch
	matchingContents := make(map[string]map[string]struct{})
	matchingFileNames := make(map[string]map[string]struct{})
	matchingCommits := make(map[string]map[string]struct{})
	for _, line := range strings.Split(string(out), "\n") {
		isFileName := false
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Split(line, " ")
			filename := parts[len(parts)-1]
			filename = strings.TrimPrefix(filename, "b/")
			curFile = filename
			curCommit = ""
			isFileName = true
		}
		if strings.HasPrefix(line, "commit ") {
			parts := strings.Split(line, " ")
			curCommit = parts[len(parts)-1]
			curFile = ""
		}
		for old, _ := range replaceMap {
			if stringContains(line, old, *ci) {
				if curFile != "" {
					matchingFiles[curFile] = false
					if isFileName {
						addMatch(matchingFileNames, curFile, old)
					} else {
						addMatch(matchingContents, curFile, old)
					}
				}
				if curCommit != "" {
					addMatch(matchingCommits, curCommit, old)
				}
			}
		}
	}
	for filename, _ := range matchingFiles {
		deleted := false
		if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
			deleted = true
		}
		matchingFiles[filename] = deleted
	}

	// if filename matches, we need to use --path-rename to fix it
	if len(matchingFileNames) > 0 {
		gitFilterRepoPrinted := false
		for filename, _ := range matchingFileNames {
			deleted := matchingFiles[filename]
			if deleted {
				continue
			}
			if !gitFilterRepoPrinted {
				fmt.Printf("git-filter-repo \\\n")
				gitFilterRepoPrinted = true
			}
			newFile := filename
			for old, new := range replaceMap {
				if strings.Contains(filename, old) {
					newFile = strings.ReplaceAll(newFile, old, new)
				}
			}
			if newFile != filename {
				fmt.Printf("  --path-rename %s:%s \\\n", filename, newFile)
			}
		}
	}
	// for deleted files, we can just completely remove them from the history.
	deletedFiles := []fileMatch{}
	notDeletedFiles := []fileMatch{}
	for filename, terms := range matchingContents {
		fm := fileMatch{
			name:  filename,
			terms: terms,
		}
		if deleted := matchingFiles[filename]; deleted {
			deletedFiles = append(deletedFiles, fm)
		} else {
			notDeletedFiles = append(notDeletedFiles, fm)
		}
	}
	if len(deletedFiles) > 0 {
		fmt.Printf("Deleted files:\n")
		for _, fm := range deletedFiles {
			fmt.Printf("  %s matches %s\n", fm.name, fm.termsString())
		}
		fmt.Printf("git-filter-repo --invert-paths \\\n")
		for _, fm := range deletedFiles {
			fmt.Printf("   --path %s \\\n", fm.name)
		}
	}
	if len(notDeletedFiles) > 0 {
		fmt.Printf("Not deleted files:\n")
		for _, fm := range notDeletedFiles {
			fmt.Printf("  %s matches %s\n", fm.name, fm.termsString())
		}
		if replaceFile != "" {
			fmt.Printf("git-filter-repo --replace-text %s\n", replaceFile)
		}
	}
	if len(matchingCommits) > 0 {
		fmt.Printf("Matching commits:\n")
		for commit, terms := range matchingCommits {
			fmt.Printf("  %s matches %s\n", commit, getKeysString(terms))
		}
		if replaceFile != "" {
			fmt.Printf("git-filter-repo --replace-message %s\n", replaceFile)
		}
	}
}

type fileMatch struct {
	name  string
	terms map[string]struct{}
}

func (s *fileMatch) termsString() string {
	return getKeysString(s.terms)
}

func getKeysString(m map[string]struct{}) string {
	terms := []string{}
	for term, _ := range m {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return strings.Join(terms, ", ")
}

func stringContains(str, substr string, ci bool) bool {
	if ci {
		return strings.Contains(strings.ToLower(str), strings.ToLower(substr))
	} else {
		return strings.Contains(str, substr)
	}
}

func addMatch(fileMap map[string]map[string]struct{}, file, term string) {
	terms, ok := fileMap[file]
	if !ok {
		terms = make(map[string]struct{})
		fileMap[file] = terms
	}
	terms[term] = struct{}{}
}
