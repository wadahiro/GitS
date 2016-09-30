package importer

import (
	"bytes"
	"fmt"
	"strings"
	// "io"
	// "io/ioutil"
	// "os"
	// "path/filepath"

	"github.com/wadahiro/gits/server/indexer"

	gitm "github.com/gogits/git-module"
	"gopkg.in/src-d/go-git.v4"
	core "gopkg.in/src-d/go-git.v4/core"
)

type GitImporter struct {
	dataDir string
	indexer indexer.Indexer
}

func NewGitImporter(dataDir string, indexer indexer.Indexer) *GitImporter {
	return &GitImporter{dataDir: dataDir, indexer: indexer}
}

func (g *GitImporter) Run(projectName string, url string) {
	fmt.Printf("Clone from %s %s\n", projectName, url)

	splitedUrl := strings.Split(url, "/")
	repoName := splitedUrl[len(splitedUrl)-1]
	repoPath := fmt.Sprintf("%s/%s/%s", g.dataDir, projectName, repoName)

	// Drop ".git" from repoName
	splitedRepoNames := strings.Split(repoName, ".git")
	if len(splitedRepoNames) > 1 {
		repoName = splitedRepoNames[0]
	}

	if err := gitm.Clone(url, repoPath,
		gitm.CloneRepoOptions{Mirror: true}); err != nil {

		// panic(err)
	}

	fmt.Println("Fetch...")
	FetchAll(repoPath)
	// git.Pull(repoPath, git.PullRemoteOptions{All: true})

	repo, err := NewGitRepo(projectName, repoName, repoPath)
	if err != nil {
		panic(err)
	}

	branches, _ := repo.GetBranches()

	for _, branch := range branches {
		fmt.Println(branch)
		g.CreateBranchIndex(repo, branch)
	}
}

func (g *GitImporter) CreateBranchIndex(repo *GitRepo, branchName string) {
	commitId, _ := repo.GetBranchCommitID(branchName)

	fmt.Println("Commit:", commitId)

	containBranches, _ := ContainsBranch(repo.Path, commitId)
	fmt.Println("ContainsBranches", containBranches)

	// commit, err := repo.GetCommit(commitId)

	commit, _ := repo.Commit(commitId)
	tree, _ := commit.Tree()

	tree.Files().ForEach(func(f *git.File) error {
		fmt.Printf("100644 blob %s %s %d\n", f.Hash, f.Name, f.Size)

		if f.Size > 1024 * 1000 * 1000 {
			return nil
		}

		blob, err := repo.Blob(f.Hash)
		if err != nil {
			return nil
		}

		reader, _ := blob.Reader()

		buf := new(bytes.Buffer)
		buf.ReadFrom(reader)
		content := buf.String()

		g.CreateFileIndex(repo.Project, repo.Repo, branchName, f.Name, f.Hash.String(), content)

		return nil
	})
}

func (g *GitImporter) CreateFileIndex(project string, repo string, branch string, filePath string, blob string, content string) {
	g.indexer.CreateFileIndex(project, repo, branch, filePath, blob, content)
}

type GitRepo struct {
	Project  string
	Repo     string
	Path     string
	gitmRepo *gitm.Repository
	repo     *git.Repository
}

func NewGitRepo(projectName string, repoName string, repoPath string) (*GitRepo, error) {
	gitmRepo, err := gitm.OpenRepository(repoPath)
	if err != nil {
		return nil, err
	}

	repo, _ := git.NewFilesystemRepository(repoPath)

	return &GitRepo{Project: projectName, Repo: repoName, Path: repoPath, gitmRepo: gitmRepo, repo: repo}, nil
}

func (r *GitRepo) GetBranches() ([]string, error) {
	return r.gitmRepo.GetBranches()
}

func (r *GitRepo) GetBranchCommitID(name string) (string, error) {
	return r.gitmRepo.GetBranchCommitID(name)
}

func (r *GitRepo) Commit(commitId string) (*git.Commit, error) {
	return r.repo.Commit(core.NewHash(commitId))
}

func (r *GitRepo) Blob(hash core.Hash) (*git.Blob, error) {
	return r.repo.Blob(hash)
}

func FetchAll(repoPath string) error {
	cmd := gitm.NewCommand("fetch")
	cmd.AddArguments("--all")
	cmd.AddArguments("--prune")

	_, err := cmd.RunInDirTimeout(-1, repoPath)
	return err
}

func ContainsBranch(repoPath string, commitId string) ([]string, error) {
	cmd := gitm.NewCommand("branch")
	cmd.AddArguments("--contains", commitId)

	stdout, err := cmd.RunInDir(repoPath)

	// fmt.Println("--------------->", err)
	if err != nil {
		return nil, err
	}
	// fmt.Println("--------------->", stdout)

	infos := strings.Split(stdout, "\n")
	// fmt.Println(len(infos))
	branches := make([]string, len(infos)-1)
	for i, info := range infos[:len(infos)-1] {
		branches[i] = info[2:]
	}
	return branches, nil
}
