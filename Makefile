include $(GOROOT)/src/Make.inc

all: install

DIRS=\
	data\
	parser\
	scanner\
	token\

TEST=\
	scanner\

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

data.install: parser.install
parser.install: token.install scanner.install
scanner.install: token.install
token.install:
