app_name = st

build: get
	CGO_ENABLED=0 go build -ldflags "-X main.VERSION=`git rev-parse --short HEAD`" -o $(app_name)

static: get
	go build -tags "netgo,osusergo,sqlite_omit_load_extension" -ldflags "-X main.VERSION=`git rev-parse --short HEAD` -linkmode external -extldflags "-static" " -o $(app_name)

get:
	go mod download

run:
	open http://127.0.0.1:3000
	go run .

test:
	go test ./... -v

clean:
	rm -f $(app_name)
test_scrapers:
	FLARESOLVERR_URL=http://127.0.0.1:8191 go test ./server/ -run TestScrapers -v -timeout 120s
