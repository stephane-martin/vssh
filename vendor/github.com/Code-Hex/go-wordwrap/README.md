# go-wordwrap(Forked)

`go-wordwrap` (Golang package: `wordwrap`) is a package for Go that
automatically wraps words into multiple lines. The primary use case for this
is in formatting CLI output, but of course word wrapping is a generally useful
thing to do.  
original: [mitchellh/go-wordwrap](https://github.com/mitchellh/go-wordwrap)
## What changed from the original

```go
wrapped := wordwrap.WrapString("foobarbaz", 3)
fmt.Println(wrapped)
```

Would output:

```
foob
arba
z
```
## Installation and Usage

Install using `go get github.com/Code-Hex/go-wordwrap`.

Full documentation is available at
http://godoc.org/github.com/Code-Hex/go-wordwrap

Below is an example of its usage ignoring errors:

```go
wrapped := wordwrap.WrapString("foo bar baz", 3)
fmt.Println(wrapped)
```

Would output:

```
foo
bar
baz
```

## Word Wrap Algorithm

This library doesn't use any clever algorithm for word wrapping. The wrapping
is actually very naive: whenever there is whitespace or an explicit linebreak.
The goal of this library is for word wrapping CLI output, so the input is
typically pretty well controlled human language. Because of this, the naive
approach typically works just fine.

In the future, we'd like to make the algorithm more advanced. We would do
so without breaking the API.
