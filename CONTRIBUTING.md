# Contributing

Corrections are the most useful contribution: if a price is wrong, open a
[price correction](../../issues/new?template=price_correction.yml) with a link to the provider's page.

## Changing a hand-checked price

1. Edit `data/manual.json`: change the number, the `source` link and the top-level `checked` date together.
2. Run `go run ./cmd/pricegen build` to regenerate `data/eu-cloud-prices.json`, the CSV and the README table.
3. Open a pull request. CI checks that every number in `manual.json` has a source.

## Changing how prices are fetched or compared

Fetchers live in `cmd/pricegen/<provider>.go`, the comparison in `cmd/pricegen/build.go`. Run `go test ./...`.
Changes to the basket rules (which servers count, what the example stack contains) change every number in the
index, so please open an issue first.

## Adding a provider

Open an issue first. A provider needs a public or token-based price API for servers and sourced unit prices for
storage, egress, load balancer, Kubernetes and PostgreSQL.

## License

Data contributions are published under CC BY 4.0, code under Apache-2.0.
