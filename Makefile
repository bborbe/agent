include Makefile.folder

DIRS = lib
DIRS += $(shell find */* -maxdepth 0 -name Makefile -exec dirname "{}" \; | grep -v lib)

formatenv:
	cat prod.env | sort > p
	mv p prod.env
	cat dev.env | sort > d
	mv d dev.env
	cat local.env | sort > l
	mv l local.env
	cat common.env | sort > c
	mv c common.env
