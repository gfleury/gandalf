// Copyright 2015 gandalf authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"os"
	"os/exec"
	"regexp"
	"time"

	bitbucketv1 "github.com/gfleury/go-bitbucket-v1"
	"github.com/globalsign/mgo"
	"github.com/tsuru/config"
	"github.com/tsuru/gandalf/bitbucketapi"

	"github.com/tsuru/tsuru/log"
)

var tempDir string

var (
	ErrRepositoryAlreadyExists = errors.New("repository already exists")
	ErrRepositoryNotFound      = errors.New("repository not found")
)

func tempDirLocation() string {
	if tempDir == "" {
		tempDir, _ = config.GetString("repository:tempDir")
	}
	return tempDir
}

// Repository represents a Git repository. A Git repository is a record in the
// database and a directory in the filesystem (the bare repository).
type Repository struct {
	Name          string `bson:"_id"`
	Users         []string
	ReadOnlyUsers []string
	IsPublic      bool
	roURL         string
	rwURL         string
}

type Links struct {
	TarArchive string `json:"tarArchive"`
	ZipArchive string `json:"zipArchive"`
}

type GitUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date"`
}

func (gu GitUser) String() string {
	return fmt.Sprintf("%s <%s>", gu.Name, gu.Email)
}

type GitCommit struct {
	Message   string
	Author    GitUser
	Committer GitUser
	Branch    string
}

type GitLog struct {
	Ref       string   `json:"ref"`
	Author    *GitUser `json:"author"`
	Committer *GitUser `json:"committer"`
	Subject   string   `json:"subject"`
	CreatedAt string   `json:"createdAt"`
	Parent    []string `json:"parent"`
}

type GitHistory struct {
	Commits []GitLog `json:"commits"`
	Next    string   `json:"next"`
}

// exists returns whether the given file or directory exists or not
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// MarshalJSON marshals the Repository in json format.
func (r *Repository) MarshalJSON() ([]byte, error) {
	data := map[string]interface{}{
		"name":    r.Name,
		"public":  r.IsPublic,
		"ssh_url": r.rwURL,
		"git_url": r.roURL,
	}
	return json.Marshal(&data)
}

// New creates a representation of a git repository. It creates a Git
// repository using the "bare-dir" setting and saves repository's meta data in
// the database.
func New(name string, users, readOnlyUsers []string, isPublic bool) (*Repository, error) {
	log.Debugf("Creating repository %q", name)
	r := &Repository{Name: name, Users: users, ReadOnlyUsers: readOnlyUsers, IsPublic: isPublic}
	if v, err := r.isValid(); !v {
		log.Errorf("repository.New: Invalid repository %q: %s", name, err)
		return r, err
	}
	client, err := bitbucketapi.Client(context.Background())
	if err != nil {
		return nil, err
	}

	//TODO: Check Response
	_, err = client.DefaultApi.CreateRepository(r.Name)
	if err != nil {
		if mgo.IsDup(err) {
			return nil, ErrRepositoryAlreadyExists
		}
		return nil, err
	}

	return r, err
}

// Get find a repository by name.
func Get(name string) (Repository, error) {
	var r Repository
	ctx, cancel := context.WithTimeout(context.Background(), 6000*time.Millisecond)
	defer cancel()
	client, err := bitbucketapi.Client(ctx)
	if err != nil {
		return r, err
	}

	// TODO: Check response
	response, err := client.DefaultApi.GetRepository("INFRA", name)
	if err != nil {
		return r, err
	}
	repo, err := bitbucketv1.GetRepositoryResponse(response)
	if err != nil {
		return r, err
	}
	r.Name = repo.Name
	r.IsPublic = repo.Public
	r.roURL = repo.Links.Clone[0].Href
	r.rwURL = repo.Links.Clone[1].Href
	if err == mgo.ErrNotFound {
		return r, ErrRepositoryNotFound
	}
	return r, err
}

// Remove deletes the repository from the database and removes it's bare Git
// repository.
func Remove(name string) error {
	log.Debugf("Removing repository %q", name)
	Client, err := bitbucketapi.Client(context.Background())
	if err != nil {
		return err
	}

	// TODO: Check response
	if _, err := Client.DefaultApi.DeleteRepository(name, name); err != nil {
		if err == mgo.ErrNotFound {
			return ErrRepositoryNotFound
		}
		return err
	}
	return nil
}

// Update update a repository data.
func Update(name string, newData Repository) error {
	log.Debugf("Updating repository %q data", name)
	_, err := Get(name)
	if err != nil {
		log.Errorf("repository.Update(%q): %s", name, err)
		return err
	}
	_, err = bitbucketapi.Client(context.Background())
	if err != nil {
		return err
	}
	// TODO: Implement Something
	return nil
}

// Validates a repository
// A valid repository MUST have:
//  - a name without any special chars only alphanumeric and underlines are allowed.
//  - at least one user in users array
// A valid repository MAY have one namespace since the following is obeyed:
//  - a namespace is optional
//  - a namespace contains only alphanumerics, underlines, @´s, -´s, +´s and
//    periods but it does not start with a period (.)
//  - one and exactly one slash (/) separates namespace and the actual name
func (r *Repository) isValid() (bool, error) {
	// The following regex validates the name of a repository, which may
	// contain a namespace. If a namespace is used, we validate it
	// accordingly (see comments above)
	m, e := regexp.Match(`^([\w-+@][\w-+.@]*/)?[\w-]+$`, []byte(r.Name))
	if e != nil {
		panic(e)
	}
	if !m {
		return false, &InvalidRepositoryError{message: "repository name is not valid"}
	}
	if len(r.Users) == 0 {
		return false, &InvalidRepositoryError{message: "repository should have at least one user"}
	}
	return true, nil
}

// GrantAccess gives full or read-only permission for users in all specified repositories.
// If any of the repositories/users does not exist, GrantAccess just skips it.
func GrantAccess(rNames, uNames []string, readOnly bool) error {
	_, err := bitbucketapi.Client(context.Background())
	if err != nil {
		return err
	}

	var info *mgo.ChangeInfo
	if readOnly {
		//info, err = Client.Repository().UpdateAll(bson.M{"_id": bson.M{"$in": rNames}}, bson.M{"$addToSet": bson.M{"readonlyusers": bson.M{"$each": uNames}}})
	} else {
		//info, err = Client.Repository().UpdateAll(bson.M{"_id": bson.M{"$in": rNames}}, bson.M{"$addToSet": bson.M{"users": bson.M{"$each": uNames}}})
	}
	if err != nil {
		return err
	}
	if info.Matched == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

// RevokeAccess revokes write permission from users in all specified
// repositories.
func RevokeAccess(rNames, uNames []string, readOnly bool) error {
	_, err := bitbucketapi.Client(context.Background())
	if err != nil {
		return err
	}

	var info *mgo.ChangeInfo
	if readOnly {
		//info, err = Client.Repository().UpdateAll(bson.M{"_id": bson.M{"$in": rNames}}, bson.M{"$pullAll": bson.M{"readonlyusers": uNames}})
	} else {
		//info, err = Client.Repository().UpdateAll(bson.M{"_id": bson.M{"$in": rNames}}, bson.M{"$pullAll": bson.M{"users": uNames}})
	}
	if err != nil {
		return err
	}
	if info.Matched == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func GetArchiveUrl(repo, ref, format string) string {
	url := "/repository/%s/archive?ref=%s&format=%s"
	return fmt.Sprintf(url, repo, ref, format)
}

type ArchiveFormat int

const (
	Zip ArchiveFormat = iota
	Tar
	TarGz
)

type ContentRetriever interface {
	GetContents(repo, ref, path string) ([]byte, error)
	GetArchive(repo, ref string, format ArchiveFormat) ([]byte, error)
	GetTree(repo, ref, path string) ([]map[string]string, error)
	GetBranches(repo string) ([]bitbucketv1.Branch, error)
	GetDiff(repo, lastCommit, previousCommit string) ([]byte, error)
	GetTags(repo string) ([]bitbucketv1.Tag, error)
	TempClone(repo string) (string, func(), error)
	Checkout(cloneDir, branch string, isNew bool) error
	AddAll(cloneDir string) error
	Commit(cloneDir, message string, author, committer GitUser) error
	Push(cloneDir, branch string) error
	GetLogs(repo, hash string, total int, path string) (*GitHistory, error)
}

var Retriever ContentRetriever

type BitBucketContentRetriever struct{}

func (*BitBucketContentRetriever) GetContents(repo, ref, path string) ([]byte, error) {
	return []byte("TODO...."), nil
}

func (*BitBucketContentRetriever) GetArchive(repo, ref string, format ArchiveFormat) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6000*time.Millisecond)
	defer cancel()
	client, err := bitbucketapi.Client(ctx)
	if err != nil {
		return nil, err
	}

	param := make(map[string]interface{})

	response, err := client.DefaultApi.GetArchive("INFRA", repo, param)
	if err != nil {
		return nil, err
	}

	return response.Values["file"].([]byte), err
}

func (*BitBucketContentRetriever) GetTree(repo, ref, path string) ([]map[string]string, error) {
	return nil, fmt.Errorf("Not implemented")
}

func (*BitBucketContentRetriever) GetBranches(repo string) ([]bitbucketv1.Branch, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6000*time.Millisecond)
	defer cancel()
	client, err := bitbucketapi.Client(ctx)
	if err != nil {
		return nil, err
	}

	param := make(map[string]interface{})

	response, err := client.DefaultApi.GetBranches("INFRA", repo, param)
	if err != nil {
		return nil, err
	}

	branches, err := bitbucketv1.GetBranchesResponse(response)
	if err != nil {
		return nil, fmt.Errorf("Error when trying to obtain tags of repository %s (%s)", repo, err)
	}

	return branches, err
}

func (*BitBucketContentRetriever) GetDiff(repo, previousCommit, lastCommit string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6000*time.Millisecond)
	defer cancel()
	client, err := bitbucketapi.Client(ctx)
	if err != nil {
		return nil, err
	}

	param := make(map[string]interface{})
	param["to"] = previousCommit
	param["from"] = lastCommit
	param["contextLines"] = int32(10)

	response, err := client.DefaultApi.StreamDiff_37("INFRA", repo, "", param)
	if err != nil {
		return nil, err
	}

	bitbucketdiffs, err := bitbucketv1.GetDiffResponse(response)
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, fmt.Errorf("Error when trying to obtain diff with commits %s and %s of repository %s (%s)", lastCommit, previousCommit, repo, err)
	}
	return json.Marshal(bitbucketdiffs)
}

func (*BitBucketContentRetriever) GetTags(repo string) ([]bitbucketv1.Tag, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6000*time.Millisecond)
	defer cancel()
	client, err := bitbucketapi.Client(ctx)
	if err != nil {
		return nil, err
	}

	param := make(map[string]interface{})

	response, err := client.DefaultApi.GetTags("INFRA", repo, param)
	if err != nil {
		return nil, err
	}

	bitbuckettags, err := bitbucketv1.GetTagsResponse(response)
	if err != nil {
		return nil, fmt.Errorf("Error when trying to obtain tags of repository %s (%s)", repo, err)
	}

	return bitbuckettags, nil
}

func (*BitBucketContentRetriever) TempClone(repo string) (cloneDir string, cleanUp func(), err error) {
	_, err = exec.LookPath("git")
	if err != nil {
		return "", nil, fmt.Errorf("Error when trying to clone repository %s (%s).", repo, err)
	}
	repoExists := true
	if err != nil || !repoExists {
		return "", nil, fmt.Errorf("Error when trying to clone repository %s (Repository does not exist).", repo)
	}
	cloneDir, err = ioutil.TempDir(tempDir, "gandalf_clone")
	if err != nil {
		return "", nil, fmt.Errorf("Error when trying to clone repository %s (Could not create temporary directory).", repo)
	}
	cleanUp = func() {
		os.RemoveAll(cloneDir)
	}
	return cloneDir, cleanUp, nil
}

func (*BitBucketContentRetriever) Checkout(cloneDir, branch string, isNew bool) error {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("Error when trying to checkout clone %s into branch %s (%s).", cloneDir, branch, err)
	}
	cloneExists, err := exists(cloneDir)
	if err != nil || !cloneExists {
		return fmt.Errorf("Error when trying to checkout clone %s into branch %s (Clone does not exist).", cloneDir, branch)
	}
	cmd := exec.Command(gitPath, "checkout")
	if isNew {
		cmd.Args = append(cmd.Args, "-b")
	}
	cmd.Args = append(cmd.Args, branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error when trying to checkout clone %s into branch %s (%s [%s]).", cloneDir, branch, err, out)
	}
	return nil
}

func (*BitBucketContentRetriever) AddAll(cloneDir string) error {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("Error when trying to add all to clone %s (%s).", cloneDir, err)
	}
	cloneExists, err := exists(cloneDir)
	if err != nil || !cloneExists {
		return fmt.Errorf("Error when trying to add all to clone %s (Clone does not exist).", cloneDir)
	}
	cmd := exec.Command(gitPath, "add", "--all")
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error when trying to add all to clone %s (%s [%s]).", cloneDir, err, out)
	}
	return nil
}

func (*BitBucketContentRetriever) Commit(cloneDir, message string, author, committer GitUser) error {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("Error when trying to commit to clone %s (%s).", cloneDir, err)
	}
	cloneExists, err := exists(cloneDir)
	if err != nil || !cloneExists {
		return fmt.Errorf("Error when trying to commit to clone %s (Clone does not exist).", cloneDir)
	}
	cmd := exec.Command(gitPath, "commit", "-m", message, "--author", author.String(), "--allow-empty-message")
	env := os.Environ()
	env = append(env, fmt.Sprintf("GIT_COMMITTER_NAME=%s", committer.Name))
	env = append(env, fmt.Sprintf("GIT_COMMITTER_EMAIL=%s", committer.Email))
	cmd.Env = env
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error when trying to commit to clone %s (%s [%s]).", cloneDir, err, out)
	}
	return nil
}

func (*BitBucketContentRetriever) Push(cloneDir, branch string) error {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("Error when trying to push clone %s (%s).", cloneDir, err)
	}
	cloneExists, err := exists(cloneDir)
	if err != nil || !cloneExists {
		return fmt.Errorf("Error when trying to push clone %s into origin's %s branch (Clone does not exist).", cloneDir, branch)
	}
	cmd := exec.Command(gitPath, "push", "origin", branch)
	cmd.Dir = cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Error when trying to push clone %s into origin's %s branch (%s [%s]).", cloneDir, branch, err, out)
	}
	return nil
}

func (*BitBucketContentRetriever) GetLogs(repo, hash string, total int, path string) (*GitHistory, error) {
	if hash == "" {
		hash = "master"
	}
	if total < 1 {
		total = 1
	}
	totalPagination := total

	ctx, cancel := context.WithTimeout(context.Background(), 6000*time.Millisecond)
	defer cancel()
	client, err := bitbucketapi.Client(ctx)
	if err != nil {
		return nil, err
	}

	param := make(map[string]interface{})
	param["limit"] = totalPagination
	if path != "" {
		param["path"] = path
	}

	response, err := client.DefaultApi.GetCommits("INFRA", repo, param)
	if err != nil {
		return nil, err
	}

	bitbucketcommits, err := bitbucketv1.GetCommitsResponse(response)
	if err != nil {
		return nil, err
	}

	objectCount := len(bitbucketcommits)

	history := GitHistory{}
	commits := make([]GitLog, objectCount)
	objectCount = 0
	for _, bitbucketcommit := range bitbucketcommits {
		commit := GitLog{}
		commit.Ref = bitbucketcommit.ID
		commit.Subject = bitbucketcommit.Message
		commit.CreatedAt = time.Unix(bitbucketcommit.CommitterTimestamp, 0).String()
		commit.Committer = &GitUser{
			Name:  bitbucketcommit.Committer.Name,
			Email: bitbucketcommit.Committer.Email,
			Date:  time.Unix(bitbucketcommit.CommitterTimestamp, 0).String(),
		}
		commit.Author = &GitUser{
			Name:  bitbucketcommit.Author.Name,
			Email: bitbucketcommit.Author.Email,
			Date:  time.Unix(bitbucketcommit.AuthorTimestamp, 0).String(),
		}
		if len(bitbucketcommit.Parents) > 0 {
			parentCount := len(bitbucketcommit.Parents)
			aux := make([]string, parentCount)
			for id, parent := range bitbucketcommit.Parents {
				aux[id] = parent.ID
			}
			commit.Parent = aux
		}
		commits[objectCount] = commit
		objectCount++
	}
	history.Commits = commits

	return &history, nil
}

func retriever() ContentRetriever {
	if Retriever == nil {
		Retriever = &BitBucketContentRetriever{}
	}
	return Retriever
}

// GetFileContents returns the contents for a given file
// in a given ref for the specified repository
func GetFileContents(repo, ref, path string) ([]byte, error) {
	return retriever().GetContents(repo, ref, path)
}

// GetArchive returns the contents for a given file
// in a given ref for the specified repository
func GetArchive(repo, ref string, format ArchiveFormat) ([]byte, error) {
	return retriever().GetArchive(repo, ref, format)
}

func GetTree(repo, ref, path string) ([]map[string]string, error) {
	return retriever().GetTree(repo, ref, path)
}

func GetBranches(repo string) ([]bitbucketv1.Branch, error) {
	return retriever().GetBranches(repo)
}

func GetDiff(repo, previousCommit, lastCommit string) ([]byte, error) {
	return retriever().GetDiff(repo, previousCommit, lastCommit)
}

func GetTags(repo string) ([]bitbucketv1.Tag, error) {
	return retriever().GetTags(repo)
}

func TempClone(repo string) (string, func(), error) {
	return retriever().TempClone(repo)
}

func Checkout(cloneDir, branch string, isNew bool) error {
	return retriever().Checkout(cloneDir, branch, isNew)
}

func AddAll(cloneDir string) error {
	return retriever().AddAll(cloneDir)
}

func Commit(cloneDir, message string, author, committer GitUser) error {
	return retriever().Commit(cloneDir, message, author, committer)
}

func Push(cloneDir, branch string) error {
	return retriever().Push(cloneDir, branch)
}

func CommitZip(repo string, z *multipart.FileHeader, c GitCommit) (*interface{}, error) {
	return nil, nil
}

func GetLogs(repo, hash string, total int, path string) (*GitHistory, error) {
	return retriever().GetLogs(repo, hash, total, path)
}

type InvalidRepositoryError struct {
	message string
}

func (err *InvalidRepositoryError) Error() string {
	return err.message
}
