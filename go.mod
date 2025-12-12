module github.com/jcchavezs/gh-iterator

go 1.24.0

toolchain go1.24.4

require (
	github.com/alexellis/go-execute/v2 v2.2.1
	github.com/spf13/afero v1.14.0
	github.com/stretchr/testify v1.10.0
	go.uber.org/goleak v1.3.0
	golang.org/x/term v0.37.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

retract (
	v0.4.3 // contains shellquotes dep although not used
	v0.4.2 // shellquotes bug
)
