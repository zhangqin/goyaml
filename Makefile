include $(GOROOT)/src/Make.inc

all: install

DIRS=\
	yaml/data\
	yaml/parser\
	yaml/scanner\
	yaml/token\

TEST=\
     yaml/scanner\

install.dirs: $(addsuffix .install, $(DIRS))
clean.dirs: $(addsuffix .clean, $(DIRS))
nuke.dirs: $(addsuffix .nuke, $(DIRS))
test.dirs: $(addsuffix .test, $(TEST))

%.install:
	+cd $* && gomake install

%.clean:
	+cd $* && gomake clean

%.nuke:
	+cd $* && gomake nuke

%.test:
	+cd $* && gomake test

install: install.dirs
clean: clean.dirs
nuke: nuke.dirs
test: test.dirs

yaml/data.install: yaml/parser.install
yaml/parser.install: yaml/token.install yaml/scanner.install
yaml/scanner.install: yaml/token.install
yaml/token.install:
