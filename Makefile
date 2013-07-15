.PHONY: all

all:
	rm -f templates/templates_data.go
	# note that right now we'll end up with .go files embedded
	# too. That seems okay for now.
	embedder templates . > templates/templates_data.go
	embedder shjs . > shjs_data.go
