# API Documentation

This folder contains the sources of proxiportd API documentation following the openapi 3.0.1 standard.

For a rendered view of the spec, use [the Swagger Petstore renderer
pointed at our spec](https://petstore.swagger.io/?url=https://raw.githubusercontent.com/proximile/proxiport/main/api-doc/openapi/openapi.yaml#/).
For local rendering while editing, see the
`npx @redocly/cli preview-docs` recipe below. The spec is not
currently embedded in the project docs site at
<https://docs.proxiport.net/>; that may change later.

## Build the documentation from the sources

There a many tools out there to convert the yaml sources into different formats. For example [Swagger Codegen](https://swagger.io/docs/open-source-tools/swagger-codegen/) or the [Open API Codegenerator](https://repo1.maven.org/maven2/org/openapitools/openapi-generator-cli/5.0.0/).
Both are java command line tools.

More comfort for reading and writing Open API docs is provided by [Redoc](https://github.com/Redocly/redoc) and there command line tool [Redoc CLI](https://redocly.com/docs/redoc/deployment/cli/).
With NodeJS installed you can directly launch the tools with `npx`. See below.

### Run a local webserver

Running a local webserver is very handy for writing the documentation. Changes to the files are immediately rendered.

```shell
cd ./api-doc/openapi
npx @redocly/cli preview-docs openapi.yaml
```

### Use the linter

Before pushing changes to the repository verify the linter does not throw errors.

```shell
cd ./api-doc/openapi
npx @redocly/cli lint openapi.yaml
```

Details about the applied rules and their output can be found
[here](https://redocly.com/docs/cli/resources/built-in-rules/).

### Render to HTML

To render the API documentation into a single dependency-free html file use:

```shell
npx redoc-cli build -o index.html openapi.yaml
```
