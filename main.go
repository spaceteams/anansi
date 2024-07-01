package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"golang.org/x/crypto/ssh/agent"
)

func main() {
	url := "git@github.com:igefa-e-business/xentral-logistics-connector.git" // Replace with the repository URL

	// Load SSH key
	sshAuth, err := sshAgent()
	if err != nil {
		log.Fatalf("failed to create ssh agent: %v", err)
	}

	// Clone the repository in memory
	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:               url,
		NoCheckout:        true,
		SingleBranch:      false,
		RecurseSubmodules: git.NoRecurseSubmodules,
		Auth:              sshAuth,
	})
	if err != nil {
		log.Fatalf("failed to clone repository: %v", err)
	}

	mainRef := getRef(r, "release/something_fishy")
	prodRef := getRef(r, "production")

	// Get the commits for the production branch and store them in a map
	productionCommits := make(map[plumbing.Hash]bool)
	productionCommitIter, err := r.Log(&git.LogOptions{From: prodRef.Hash()})
	if err != nil {
		log.Fatalf("Failed to get production branch commits: %v", err)
	}

	err = productionCommitIter.ForEach(func(c *object.Commit) error {
		productionCommits[c.Hash] = true
		return nil
	})
	if err != nil {
		log.Fatalf("Failed to iterate through production commits: %v", err)
	}

	// Get the commit history from the main branch
	mainCommitIter, err := r.Log(&git.LogOptions{From: mainRef.Hash(), Order: git.LogOrderDFSPost})
	if err != nil {
		log.Fatalf("failed to get commits: %v", err)
	}

	pattern := regexp.MustCompile(`Merge pull request #(\d+) from [^/]+/([^/]+)/(.+)`)
	err = mainCommitIter.ForEach(func(c *object.Commit) error {
		_, found := productionCommits[c.Hash]
		if found {
			return nil
		}

		msg := c.Message
		firstNewLine := strings.IndexByte(msg, '\n')
		if firstNewLine > 0 {
			msg = msg[:firstNewLine]
		}
		if len(c.ParentHashes) > 1 {
			matches := pattern.FindStringSubmatch(msg)
			if len(matches) == 4 {
				prNumber := matches[1]
				branchType := matches[2]
				description := matches[3]
				fmt.Printf("%s %s (#%s)\n", branchType, description, prNumber)
			}
		} else {
			fmt.Printf("+ %s\n", msg)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("failed to iterate commits: %v", err)
	}
}

func getRef(r *git.Repository, name string) *plumbing.Reference {
	mainRefName := plumbing.NewBranchReferenceName(name)
	_, err := r.Reference(mainRefName, true)
	if err != nil {
		mainRemoteRefName := plumbing.NewRemoteReferenceName("origin", name)
		mainRemoteRef, err := r.Reference(mainRemoteRefName, true)
		if err != nil {
			log.Fatalf("failed to get main remote branch reference: %v", err)
		}
		mainRef := plumbing.NewHashReference(mainRefName, mainRemoteRef.Hash())
		r.Storer.SetReference(mainRef)
	}
	mainRef, err := r.Reference(plumbing.NewBranchReferenceName(name), true)
	if err != nil {
		log.Fatalf("failed to get main branch reference: %v", err)
	}
	return mainRef
}

// sshAgent sets up the SSH authentication using the ssh-agent
func sshAgent() (*ssh.PublicKeys, error) {
	sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if sshAuthSock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}

	conn, err := net.Dial("unix", sshAuthSock)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH_AUTH_SOCK: %v", err)
	}

	agentClient := agent.NewClient(conn)
	signers, err := agentClient.Signers()
	if err != nil {
		return nil, fmt.Errorf("failed to get signers from agent: %v", err)
	}

	if len(signers) == 0 {
		return nil, fmt.Errorf("no signers found in SSH agent")
	}

	fmt.Printf("Using SSH signer: %v\n", signers[0].PublicKey())

	return &ssh.PublicKeys{User: "git", Signer: signers[0]}, nil
}
