# Introduction #

goyaml consists of four packages: data, parser, scanner, and token. They must all be present for a proper goyaml install.


# goinstall approach #

Run the following at your system's command prompt:

```
goinstall goyaml.googlecode.com/hg/token
goinstall goyaml.googlecode.com/hg/scanner
goinstall goyaml.googlecode.com/hg/parser
goinstall goyaml.googlecode.com/hg/data
```

Due to restrictions in how goinstall works, there is no other convenient way to install these packages.

# Makefile approach #

[Checkout the source](http://code.google.com/p/goyaml/source/checkout) from Mercurial, and then run:

```
make install
```

to install goyaml.