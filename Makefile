.PHONY: all

all:
	rm -f templates/templates_data.go
	# note that right now we'll end up with .go files embedded
	# too. That seems okay for now.
	(cd templates && embedder templates . > templates_data.go)
	rm -f third_party/shjs/shjs_data.go
	(cd third_party/shjs/ && embedder shjs . > shjs_data.go)
