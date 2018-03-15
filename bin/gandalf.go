// Copyright 2014 gandalf authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"log/syslog"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/tsuru/config"
	"github.com/tsuru/gandalf/repository"
	"github.com/tsuru/gandalf/user"
)

var log *syslog.Writer

func hasWritePermission(u *user.User, r *repository.Repository) (allowed bool) {
	for _, userName := range r.Users {
		if u.Name == userName {
			return true
		}
	}
	return false
}

func hasReadPermission(u *user.User, r *repository.Repository) (allowed bool) {
	if r.IsPublic {
		return true
	}
	for _, userName := range r.Users {
		if u.Name == userName {
			return true
		}
	}
	for _, userName := range r.ReadOnlyUsers {
		if u.Name == userName {
			return true
		}
	}
	return false
}

// Checks whether a command is a valid git command
// The following format is allowed:
// (git-[a-z-]+) '/?([\w-+@][\w-+.@]*/)?([\w-]+)\.git'
func parseGitCommand() (command, name string, err error) {
	// The following regex validates the git command, which is in the form:
	//    <git-command> [<namespace>/]<name>
	// with namespace being optional. If a namespace is used, we validate it
	// according to the following:
	//  - a namespace is optional
	//  - a namespace contains only alphanumerics, underlines, @´s, -´s, +´s
	//    and periods but it does not start with a period (.)
	//  - one and exactly one slash (/) separates namespace and the actual name
	r, err := regexp.Compile(`(git-[a-z-]+) '/?([\w-+@][\w-+.@]*/)?([\w-]+)\.git'`)
	if err != nil {
		panic(err)
	}
	m := r.FindStringSubmatch(os.Getenv("SSH_ORIGINAL_COMMAND"))
	if len(m) != 4 {
		return "", "", errors.New("You've tried to execute some weird command, I'm deliberately denying you to do that, get over it.")
	}
	return m[1], m[2] + m[3], nil
}

func formatCommand() ([]string, error) {
	p, err := config.GetString("git:bare:location")
	if err != nil {
		log.Err(err.Error())
		return []string{}, err
	}
	_, repoName, err := parseGitCommand()
	if err != nil {
		log.Err(err.Error())
		return []string{}, err
	}
	repoName += ".git"
	cmdList := strings.Split(os.Getenv("SSH_ORIGINAL_COMMAND"), " ")
	if len(cmdList) != 2 {
		log.Err("Malformed git command")
		return []string{}, fmt.Errorf("Malformed git command")
	}
	cmdList[1] = path.Join(p, repoName)
	return cmdList, nil
}

func main() {
	var err error
	log, err = syslog.New(syslog.LOG_INFO, "gandalf-listener")
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		panic(err.Error())
	}
	err = config.ReadConfigFile("/etc/gandalf.conf")
	if err != nil {
		log.Err(err.Error())
		fmt.Fprintln(os.Stderr, err.Error())
		return
	}
	_, _, err = parseGitCommand()
	if err != nil {
		log.Err(err.Error())
		fmt.Fprintln(os.Stderr, err.Error())
		return
	}
}
