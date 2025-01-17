# Github Iterator

This library allows to iterate github repositories in an organization. It is useful to quickly run tools across repositories and come up with reports, rollout changes or open PRs with a certain change. A good example is the [govulcheck](./examples/govulncheck/) example.

## Getting started

```go
import (
	...
	iterator "github.com/jcchavezs/gh-iterator"
)

func main() {
	err = iterator.RunForOrganization("my-org", iterator.Filters{}, func(repository string, exec exec.Execer) error {
        fmt.Printf("Hello world from %s", repository)
		return nil
	}, iterator.Options{})
	if err != nil {
		log.Fatal(err)
	}
}
```
