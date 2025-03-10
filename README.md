# Github Iterator

This library allows to iterate github repositories in an organization. It is useful to quickly run tools across repositories and come up with reports, rollout changes or open PRs with a certain change. Check the [examples](./examples/) to see it in action.

## Getting started

```go
import (
	...
	iterator "github.com/jcchavezs/gh-iterator"
)

func main() {
	err = iterator.RunForOrganization(context.Background(), org, iterator.SearchOptions{}, func(ctx context.Context, repository string, isEmpty bool, exec exec.Execer) error {
        fmt.Printf("Hello world from %s", repository)
		return nil
	}, iterator.Options{})
	if err != nil {
		log.Fatal(err)
	}
}
```
