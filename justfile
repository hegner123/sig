binary := "sig"
install_path := "/usr/local/bin"

build:
    go build -o {{binary}}
    codesign -s - {{binary}}

install: build
    sudo cp {{binary}} {{install_path}}/
    sudo codesign -f -s - {{install_path}}/{{binary}}

clean:
    rm -f {{binary}}

test:
    go test -v
