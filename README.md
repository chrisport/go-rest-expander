# Go REST Expander: Walkex

This library is based on (Go REST expander)[https://github.com/isa/go-rest-expander].
It provides a simple interface to walk over an interface and resolve data-references with the actual data from other
resources.

# Resolver interface
Any implementation of the Resolver interface can be added to Walkex in order to detect and resolve references.
The project contains ```MongoDbRefResolver```as example. It resolves MongoDB References by making a GET-Request on a configured URL.

```
uris := map[string]string{"profiles: "http://api.application.com/profile/id/"}
walkex.AddResolver(NewMongoDbRefResolver(uris, false))
``

## License
Licensed under [Apache 2.0](LICENSE).
