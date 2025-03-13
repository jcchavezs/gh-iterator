# Github Iterator

This library allows to iterate and run a processor function in the context of github repositories in an organization or a single repository. It is useful to quickly build prototypes or automations that run tools across repositories and come up with reports, open PRs with certain changes or just run checks. Check the [examples](./examples/) to see it in action.

## Getting started

```go
import (
	...
	iterator "github.com/jcchavezs/gh-iterator"
)

func processor(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
	fmt.Printf("Hello world from %s", repository)
	return nil
}

func main() {
	// ...
	err = iterator.RunForOrganization(
		context.Background(),
		org,
		iterator.SearchOptions{},
		processor,
		iterator.Options{},
	)
	if err != nil {
		log.Fatal(err)
	}
}
```
