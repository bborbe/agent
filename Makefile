include Makefile.folder

DIRS = lib
DIRS += $(shell find */* -maxdepth 0 -name Makefile -exec dirname "{}" \; | grep -v lib)
