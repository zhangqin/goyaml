include $(GOROOT)/src/Make.inc

all: install

install:
	cd src && gomake install

clean:
	cd src && gomake clean

nuke:
	cd src && gomake nuke

test:
	cd src && gomake test
