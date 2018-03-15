// Copyright 2014 gandalf authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hook

import (
	"strings"

	"github.com/tsuru/config"
)

func createHookFile(path string, content []byte) error {

	return nil
}

// Adds a hook script.
func Add(name string, repos []string, content []byte) error {
	configParam := "git:bare:template"
	if len(repos) > 0 {
		configParam = "git:bare:location"
	}
	path, err := config.GetString(configParam)
	if err != nil {
		return err
	}
	if len(repos) > 0 {
		for _, repo := range repos {
			repo += ".git"
			s := []string{path, repo, "hooks"}
			dirPath := strings.Join(s, "/")
			s = []string{dirPath, name}
			scriptPath := strings.Join(s, "/")
			err = createHookFile(scriptPath, content)
			if err != nil {
				return err
			}
		}
	} else {
		s := []string{path, "hooks", name}
		scriptPath := strings.Join(s, "/")
		return createHookFile(scriptPath, content)
	}
	return nil
}
