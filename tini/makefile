all: tini static

tini: tini.c
	gcc -o tini tini.c

tini-static: tini.c
	gcc --static -o tini-static tini.c

static: tini-static

clean:
	rm -f tini tini-static

