.PHONY: all

all:
	rm -f shjs_data.go shjs_date.go
	# note that right now we'll end up with .go files embedded
	# too. That seems okay for now.
	embedder shjs . > shjs_data.go
	echo package shjs >> shjs_date.go
	echo 'var Timestamp = "'`date`'"' >> shjs_date.go
