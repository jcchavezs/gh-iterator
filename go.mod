module github.com/jcchavezs/gh-iterator

go 1.26.0

require (
	github.com/alexellis/go-execute/v2 v2.2.1
	github.com/spf13/afero v1.15.0
	github.com/stretchr/testify v1.12.1
	go.uber.org/goleak v1.3.0
	golang.org/x/term v0.47.0
)

require (
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.28.0 // indirect
)

retract (
	v0.4.3 // contains shellquotes dep although not used
	v0.4.2 // shellquotes bug
)
