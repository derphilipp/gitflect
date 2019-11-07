package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"sync"

	"log"
	"os"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
	"gopkg.in/yaml.v3"
)

const githubURL = "https://github.com/"

var (
	configFile = flag.String("config-file", "config.yaml", "path to custom configuration file")
	// mazeFile   = flag.String("maze-file", "maze01.txt", "path to a custom maze file")
)

type Project struct {
	LocalName string `yaml:"local_name"`
	OriginURL string `yaml:"origin_url"`
}

type Config struct {
	DefaultURL     string             `yaml:"default_url"`
	DataPath       string             `yaml:"data_path"`
	SSHKey         string             `yaml:"ssh_key"`
	Parallel       bool               `yaml:"parallel"`
	GithubProjects []string           `yaml:"github_projects"`
	Projects       map[string]Project `yaml:"projects"`
}

var PublicKey *ssh.PublicKeys
var cfg Config

func loadConfig() error {

	f, err := os.Open(*configFile)
	if err != nil {
		return err
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		return err
	}

	sshFile, err := ioutil.ReadFile(cfg.SSHKey)
	if err != nil {
		fmt.Printf("Could not load %s:\n", cfg.SSHKey)
		return err
	}
	publicKey, err := ssh.NewPublicKeys("git", []byte(sshFile), "")
	if err != nil {
		fmt.Println(err)
		return err
	}
	PublicKey = publicKey

	return nil
}

func clone(url, localPath string) error {
	//	null, _ := os.Open(os.DevNull)
	err := os.MkdirAll(localPath, os.ModePerm)
	if err == os.ErrExist {
	} else if err != nil {
		fmt.Println("Could not create directory")
		return err
	}
	// Clone
	_, err = git.PlainClone(localPath, true, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
	})

	if err == git.ErrRepositoryAlreadyExists {
		log.Printf("Repo already exists, nice...")
	} else if err != nil {
		log.Printf("Cloning failed: %v\n", err)
		return err
	}
	return nil
}

func loadRepo(path string) (*git.Repository, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		log.Printf("Loading repository failed: %v\n", err)
		return nil, err
	}
	return repo, nil
}

func fetch(repo *git.Repository) error {
	// Fetch
	err := repo.Fetch(&git.FetchOptions{})
	if err == git.NoErrAlreadyUpToDate {
		log.Printf("Already up to date")
	} else if err != nil {
		log.Printf("Fetching existing repo failed: %v\n", err)
		return err
	}
	return nil
}

func setRemote(repo *git.Repository, targetURL string) error {
	_, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "gittig",
		URLs: []string{targetURL}})
	return err
}

func push(repo *git.Repository) error {
	// Push
	err := repo.Push(&git.PushOptions{
		RemoteName: "gittig",
		Auth:       PublicKey,
	})
	return err
}

func processProject(url, localname string) error {
	fmt.Printf("Processing Project: %s\n\n", localname)

	targetURL := cfg.DefaultURL + localname
	projectPath := cfg.DataPath + localname

	clone(url, projectPath)
	repo, err := loadRepo(projectPath)
	fetch(repo)
	repo.DeleteRemote("gittig")
	setRemote(repo, targetURL)
	push(repo)

	if err != nil {
		log.Printf("Pushing failed!: %v\n", err)
		return err
	}
	return nil
}

func main() {
	flag.Parse()
	err := loadConfig()
	if err != nil {
		log.Printf("Error loading configuration: %v\n", err)
		return
	}
	if _, err := os.Stat(cfg.DataPath); os.IsNotExist(err) {
		log.Printf("Data directory does not exist.")
	}

	wg := sync.WaitGroup{}
	for projectkey := range cfg.Projects {
		project := cfg.Projects[projectkey]
		if cfg.Parallel {
			go processProject(project.OriginURL, project.LocalName)
			wg.Add(1)
		} else {
			processProject(project.OriginURL, project.LocalName)
		}
	}

	for _, project := range cfg.GithubProjects {
		if cfg.Parallel {
			go processProject(githubURL+project, project)
			wg.Add(1)
		} else {
			processProject(githubURL+project, project)
		}
	}
	if cfg.Parallel {
		wg.Wait()
	}

}
