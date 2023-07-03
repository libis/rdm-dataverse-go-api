# rdm-dataverse-go-api
Go API for Dataverse

To use the API, add this to your ```go.mod``` (change the version to the latest released version):
```
require (
	github.com/libis/rdm-dataverse-go-api v1.0.1
)
```

See [main.go](examples/main.go) for usage examples. If you want to run these examples, simply clone the repository and run:
```
go run ./examples
```
Replace the content of this variable to point to your own Dataverse server:
```
var server = "https://demo.dataverse.org"
```
Replace the content of the following variable with your own API key to run the examples using the API token access:
```
var apiToken = "your-api-token"
```
For testing with URL signing i.s.o. token access, replace these variables content with the real credentials:
```
var user = "user-name"
var adminApiKey = "admin-api-key"
var unblockKey = "unblock-key"
```
You can then replace ```TokenAccessClient()``` with ```UrlSigningClient()```.
