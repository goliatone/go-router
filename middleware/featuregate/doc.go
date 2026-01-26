// Package featuregate provides go-router middleware for wiring go-featuregate
// scope and actor data into request contexts. This package is the only place in
// go-router that should import go-featuregate so the core router stays
// dependency-free.
package featuregate
