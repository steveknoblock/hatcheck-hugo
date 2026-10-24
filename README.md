# hatcheck-hugo

Turns a [Hatcheck](https://github.com/steveknoblock/hatcheck-go) export into a directory of Markdown files laid out for [Hugo](https://gohugo.io) (or any static site generator that reads a plain directory of `.md` files).

It reads only the `.tar.gz` archive that Hatcheck's export produces. It never talks to a running Hatcheck server and never reads Hatcheck's on-disk store, so it makes no API calls and needs no credentials.

This used to be `hatcheck export-markdown` inside Hatcheck. It moved out because publishing to a static site is an application concern, not storage or query.

## Usage

Export a namespace from Hatcheck, then render it:

```sh
hatcheck export -source bob -namespace bob      # writes bob.tar.gz
hatcheck-hugo bob.tar.gz                        # writes ./bob/
hatcheck-hugo -o ./content bob.tar.gz           # writes ./content/
```

The same archive is available over HTTP as `GET /export?source=bob&namespace=bob` (read permission), which is how the Hatcheck UI produces it.

```
hatcheck-hugo [-namespace <ns>] [-o <dir>] <archive.tar.gz>
```

Flags must come before the archive path.

| Flag | Meaning |
| --- | --- |
| `-namespace` | Namespace to render. Defaults to the namespace recorded in the archive's manifest. Required for a full (unscoped) export. |
| `-o` | Output directory. Defaults to `./<namespace>`. |

Existing files in the output directory with the same names are overwritten. Nothing else there is removed.

## Output

- A name that points at plain text becomes `<slug>.md`.
- A name that points at a Collection becomes a directory `<slug>/` with an `_index.md` and one entry per member.
- A Collection member that is also a name in the namespace is rendered under that name. A member with no name falls back to the first 8 characters of its hash.
- A Relation becomes a small descriptive file instead of being dropped.
- Front matter (`title`, `date`, `tags`) is written wherever there is a real title. An unnamed member gets plain content with no front matter. Tags are the `#hashtags` found in the text.
- Names that slugify to the same string get `-2`, `-3`, and so on.
- Content reachable from two places is written in both places.

## Limits

- It reads archive format **version 1**. An archive with any other `manifest.json` version is refused.
- The whole archive is read into memory. That is fine for a namespace-scoped export; a full export of a very large store would need a streaming approach this tool does not have.
- It depends on the export archive's layout (`manifest.json`, `objects/`, `metadata/log.json`) and on the log's `name-create` and `name-update` entries.

## Development

The tool uses only the Go standard library.

```sh
go build ./...
go test ./...
```
