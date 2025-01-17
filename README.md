# Github Iterator

This library allows to iterate github repositories in an organization.

## Getting started

```go
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
