// Copyright 2015 gandalf authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package user

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	bitbucketv1 "github.com/gfleury/go-bitbucket-v1"
	"github.com/globalsign/mgo"
	"github.com/tsuru/gandalf/bitbucketapi"
)

var (
	ErrDuplicateKey = errors.New("Duplicate key")
	ErrInvalidKey   = errors.New("Invalid key")
	ErrKeyNotFound  = errors.New("Key not found")
)

type Key bitbucketv1.SSHKey

func addKey(name, body, username string) error {
	client, err := bitbucketapi.Client(context.Background())
	if err != nil {
		return err
	}

	//TODO: check response
	param := make(map[string]interface{})
	param["user"] = username
	param["text"] = body
	_, err = client.DefaultApi.CreateSSHKey(param)
	if err != nil {
		if mgo.IsDup(err) {
			return ErrDuplicateKey
		}
		return err
	}
	return nil
}

func updateKey(name, body, username string) error {
	return nil
}

func addKeys(keys map[string]string, username string) error {
	for name, k := range keys {
		err := addKey(name, k, username)
		if err != nil {
			return err
		}
	}
	return nil
}

func remove(k *Key) error {
	//TODO
	return nil
}

func removeUserKeys(username string) error {
	//TODO
	return nil
}

// removes a key from the database and the authorized_keys file.
func removeKey(id int, username string) error {
	//TODO
	return nil
}

type KeyList []bitbucketv1.SSHKey

func (keys KeyList) MarshalJSON() ([]byte, error) {
	m := make(map[string]string, len(keys))
	for _, key := range keys {
		m[key.Label] = key.String()
	}
	return json.Marshal(m)
}

// ListKeys lists all user's keys.
//
// If the user is not found, returns an error
func ListKeys(uName string) (KeyList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6000*time.Millisecond)
	defer cancel()
	client, err := bitbucketapi.Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := client.DefaultApi.GetSSHKeys(uName)
	keys, err := bitbucketv1.GetSSHKeysResponse(response)
	return KeyList(keys), err
}
