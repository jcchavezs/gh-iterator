// This example lists all repositories of an organization with ListForOrganization and checks
// if each one has a dependabot manifest by reading .github/dependabot.yml with github.ReadFile
// (no clone, os.ErrNotExist means missing). It also shows AllPages and counting with a sync.Mutex.
package main
