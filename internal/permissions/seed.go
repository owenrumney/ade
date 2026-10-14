package permissions

import "slices"

// seeds are what a first run starts with, written to the file rather than
// compiled in: they are the only rules never shown on a card.
var seeds = []Rule{
	"Bash(cd:*)",
	"Bash(go test:*)",
	"Bash(go build:*)",
	"Bash(go vet:*)",
	"Bash(go doc:*)",
	"Bash(gofmt:*)",
	"Bash(golangci-lint:*)",
	"Bash(make:*)",
	"Bash(npm run:*)",
	"Bash(npx vitest:*)",
	"Bash(cat:*)",
	"Bash(head:*)",
	"Bash(tail:*)",
	"Bash(wc:*)",
	"Bash(sort:*)",
	"Bash(uniq:*)",
	"Bash(ls:*)",
	"Bash(pwd:*)",
	"Bash(file:*)",
	"Bash(which:*)",
	"Bash(grep:*)",
	"Bash(rg:*)",
	"Bash(sed -n:*)",
	"Bash(git status:*)",
	"Bash(git diff:*)",
	"Bash(git log:*)",
	"Bash(git show:*)",
	"Bash(git branch:*)",
}

// seedDeny is what a seeded file refuses outright. Narrow on purpose: a deny
// rule nobody chose has to be something nobody would choose.
var seedDeny = []Rule{
	"Bash(git push --force:*)",
	"Bash(rm -rf /:*)",
}

// seeded is the file a first run writes.
func seeded() File {
	return File{Version: Version, Allow: slices.Clone(seeds), Deny: slices.Clone(seedDeny)}
}
