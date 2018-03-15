// Copyright 2014 gandalf authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"fmt"

	"github.com/tsuru/gandalf/bitbucketapi"
	"github.com/tsuru/gandalf/user"
)

func getUserOr404(name string) (user.User, error) {
	var u user.User
	conn, err := bitbucketapi.Client(context.Background())
	if err != nil {
		return u, err
	}

	// TODO: Check response
	if _, err := conn.DefaultApi.GetUser(name); err != nil && err.Error() == "not found" {
		return u, fmt.Errorf("User %s not found", name)
	}
	return u, nil
}
