// Copyright 2015 gandalf authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package user

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/globalsign/mgo"
	"github.com/tsuru/gandalf/bitbucketapi"
	"github.com/tsuru/tsuru/log"
)

var (
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")

	userNameRegexp = regexp.MustCompile(`\s|[^aA-zZ0-9-+.@]|(^$)`)
)

type User struct {
	Name string `bson:"_id"`
}

// Creates a new user and write his/her keys into authorized_keys file.
//
// The authorized_keys file belongs to the user running the process.
func New(name string, keys map[string]string) (*User, error) {

	client, err := bitbucketapi.Client(context.Background())
	if err != nil {
		return nil, err
	}

	bbname := strings.Split(name, "@")[0]
	_, err = client.DefaultApi.GetUser(bbname)
	if err != nil {
		if mgo.IsDup(err) {
			return nil, ErrDuplicateKey
		}
		return nil, err
	}

	log.Debugf(`Creating user "%s"`, name)
	u := &User{Name: name}
	return u, nil
}

func (u *User) isValid() (isValid bool, err error) {
	if userNameRegexp.MatchString(u.Name) {
		return false, &InvalidUserError{message: "username is not valid"}
	}
	return true, nil
}

// Removes a user.
// Also removes it's associated keys from authorized_keys and repositories
// It handles user with repositories specially when:
// - a user has at least one repository:
//     - if he/she is the only one with access to the repository, the removal will stop and return an error
//     - if there are more than one user with access to the repository, gandalf will first revoke user's access and then remove the user permanently
// - a user has no repositories: gandalf will simply remove the user
func Remove(name string) error {
	//var u *User
	//TODO
	return nil
}

func (u *User) handleAssociatedRepositories() error {
	//TODO
	return nil
}

// AddKey adds new SSH keys to the list of user keys for the provided username.
//
// Returns an error in case the user does not exist.
func AddKey(username string, k map[string]string) error {
	return addKeys(k, username)
}

// UpdateKey updates the content of the given key.
func UpdateKey(username string, k Key) error {
	//TODO
	return nil
}

// RemoveKey removes the key from the database and from authorized_keys file.
//
// If the user or the key is not found, returns an error.
func RemoveKey(username string, keyname int) error {
	return removeKey(keyname, username)
}

type InvalidUserError struct {
	message string
}

func (err *InvalidUserError) Error() string {
	return err.message
}
