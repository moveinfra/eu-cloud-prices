# EU Cloud Price Index

What the same infrastructure costs on **AWS, Azure and Google Cloud** and on the **EU-owned clouds UpCloud, Hetzner,
Scaleway and OVHcloud**, in euros, refreshed every month from the providers' own price lists.

- **Open data**: [`data/eu-cloud-prices.json`](data/eu-cloud-prices.json) (every server type plus unit prices) and
  [`data/eu-cloud-prices.csv`](data/eu-cloud-prices.csv) (the table below). CC BY 4.0.
- **Reproducible**: `go run ./cmd/pricegen fetch all && go run ./cmd/pricegen build` regenerates everything.
- **Sourced**: server prices come from price APIs; every other number has the page it was taken from in
  [`data/manual.json`](data/manual.json).

## The index

<!-- prices:start -->
_EUR per month, excluding VAT. Server prices fetched 2026-10-28, other prices checked 2026-10-28, USD at 1.1133 per EUR (ECB, 2026-10-28). Lowest in bold._

| | AWS | Azure | Google Cloud | UpCloud 🇪🇺 | Hetzner 🇪🇺 | Scaleway 🇪🇺 | OVHcloud 🇪🇺 | 
|---|--:|--:|--:|--:|--:|--:|--:|
| Server, 2 vCPU, 8 GB | €67.86 | €66.80 | €56.61 | €38.00 | €42.99 | €40.95 | **€37.38** | 
| Server, 4 vCPU, 16 GB | €136 | €134 | €113 | €92.00 | €85.99 | **€67.76** | €74.68 | 
| Server, 8 vCPU, 32 GB | €271 | €267 | €226 | €192 | **€138** | €139 | €149 | 
| Server, 16 vCPU, 64 GB | €543 | €534 | €453 | €384 | **€276** | €280 | €299 | 
| Block storage, 1 TB SSD | €85.50 | €85.20 | €108 | €220 | **€44.00** | €94.90 | €86.00 | 
| Object storage, 1 TB | €22.00 | €17.60 | €20.70 | €20.00 | **€6.40** | €16.10 | €14.00 | 
| Internet egress, 1 TB from one server | €72.72 | €70.29 | €108 | **included** | **included** | **included** | **included** | 
| Internet egress, 10 TB from one server | €800 | €773 | €997 | **included** | **included** | **included** | **included** | 
| Managed load balancer | €17.70 | €16.39 | €16.39 | €30.00 | €7.49 | €16.79 | **€6.00** | 
| Managed Kubernetes control plane | €65.57 | €65.57 | €65.57 | **€60.00** | – | €80.30 | €65.70 | 
| Managed PostgreSQL, 2 vCPU, 8 GB | €131 | €139 | €109 | **€60.00** | – | €80.30 | €143 | 
| **Example stack**¹ | €774 | €765 | €747 | €496 | **€337** | €364 | €430 | 

¹ 3 servers with 4 vCPU and 16 GB, 500 GB block storage, 1 TB object storage, 2 TB internet egress, one load balancer and managed PostgreSQL with 2 vCPU and 8 GB. Where a provider has no managed PostgreSQL, the stack runs it on one more server.

<details><summary>Which server each basket picked</summary>

| | AWS | Azure | Google Cloud | UpCloud | Hetzner | Scaleway | OVHcloud | 
|---|---|---|---|---|---|---|---|
| Server, 2 vCPU, 8 GB | `m6a.large` | `Standard_D2a_v4` | `e2-standard-2` | `PREMIUM-2xCPU-8GB` | `CCX13` | `PRO2-XXS` | `b3-8` | 
| Server, 4 vCPU, 16 GB | `m6a.xlarge` | `Standard_D4a_v4` | `e2-standard-4` | `PREMIUM-4xCPU-16GB` | `CCX23` | `GP1-XS` | `b3-16` | 
| Server, 8 vCPU, 32 GB | `m6a.2xlarge` | `Standard_D8a_v4` | `e2-standard-8` | `PREMIUM-8xCPU-32GB` | `CCX33` | `GP1-S` | `b3-32` | 
| Server, 16 vCPU, 64 GB | `m6a.4xlarge` | `Standard_D16a_v4` | `e2-standard-16` | `PREMIUM-16xCPU-64GB` | `CCX43` | `GP1-M` | `b3-64` | 

</details>
<!-- prices:end -->

## How it is measured

- List prices in EUR per month, excluding VAT, on-demand, without discounts, savings plans or free tiers.
- A month is 730 hours, or less when the provider caps billing below that (UpCloud: 672 hours).
- USD prices are converted at the European Central Bank reference rate of the date shown.
- Locations: AWS `eu-central-1`, Azure `germanywestcentral`, Google Cloud `europe-west3` (all Frankfurt),
  UpCloud `de-fra1`, Hetzner `fsn1`, Scaleway `fr-par-1`, OVHcloud Public Cloud (same price across its EU regions).
- **Server baskets** take the cheapest x86 server with at least the stated vCPU and memory from general-purpose,
  compute- or memory-optimised ranges. Shared-core and burstable servers (AWS `t*`, Azure `B*`, Hetzner `CX`/`CPX`,
  Scaleway `BASIC`/`DEV`) are left out, and a vCPU counts as a vCPU. Real performance per vCPU differs between
  providers; benchmark your own workload before you decide.
- **Egress** includes each provider's free allowance. Several EU providers include traffic with every server, so
  "included" means no charge at that volume.
- Azure's price API has no memory figures; Azure memory is derived from the VM size name (marked `estimated` in the JSON).
- Google Cloud machine prices come from [google-cloud-pricing-cost-calculator](https://github.com/Cyclenerd/google-cloud-pricing-cost-calculator)
  (Apache-2.0), which generates them from Google's Cloud Billing Catalog API.

Spotted a wrong number? [Open an issue](../../issues/new?template=price_correction.yml) with a link to the provider's page.

## Using the data

```sh
curl -sL https://raw.githubusercontent.com/moveinfra/eu-cloud-prices/main/data/eu-cloud-prices.json \
  | jq '.baskets[] | select(.id=="server-4-16") | .prices | map_values(.monthly)'
```

The JSON has four parts:

| Key | What it holds |
|---|---|
| `providers` | id, name, whether EU-owned, location, where the server list came from and when |
| `baskets` | the table above, with the server or product each cell used |
| `services` | unit prices per provider: block and object storage per GB-month, egress tiers, load balancer, Kubernetes control plane, managed PostgreSQL, each with its source |
| `servers` | every on-demand Linux server type per provider: vCPU, memory, monthly EUR, category, architecture |

The [cloud bill analyzer](https://github.com/moveinfra/cloud-bill-analyzer) and the
[cloud exit fee calculator](https://moveinfra.eu/) use this file.

## Refreshing

A [workflow](.github/workflows/prices.yml) runs on the 3rd of every month and opens a pull request. UpCloud and
Hetzner need read-only API tokens as the repository secrets `UPCLOUD_TOKEN` and `HCLOUD_TOKEN`; without them the last
fetched lists are kept. Prices in `data/manual.json` are checked by hand: update the value, the `source` link and
`checked` date together.

```sh
go run ./cmd/pricegen fetch aws        # one provider -> data/sources/aws.json (AWS streams ~400 MB)
go run ./cmd/pricegen fetch all        # all providers whose credentials are set, plus ECB rates
go run ./cmd/pricegen build            # data/eu-cloud-prices.{json,csv} and the table in this README
go test ./...
```

## License

Data (`data/`): [CC BY 4.0](LICENSE-DATA), credit "EU Cloud Price Index by MoveInfra". Code: [Apache-2.0](LICENSE-CODE).

Maintained by [MoveInfra](https://moveinfra.eu/), which moves companies from US clouds to EU-owned ones. Provider
names are trademarks of their owners; this index is not affiliated with any of them.
