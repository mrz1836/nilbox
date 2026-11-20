// Package catalog is Autopilot's vocabulary: the triggers a routine fires on and a
// schedule's choices, the fields its conditions read and the operators each type of field
// takes, the steps it can run and which dispatcher runs each, the agent step's settings,
// the tools presets, the other steps' choices (how Merge it merges, how a review posts,
// what Tell me says), the limits and their ranges, and the words the builder shows for
// every one of them.
//
// Every name a routine's spec stores is a constant here, so the spec, the evaluator, and
// the builder spell each one the same way, and a later build that adds an entry adds it
// here once. The catalog is plain data: it reads nothing and decides nothing. A step is
// listed before the code that runs it exists, marked not Offered, so the builder offers
// only what is built while the spec's format already holds every step.
//
// Autopilot never closes anything, approves workflow runs, or merges past GitHub's
// rules, so the catalog has no entry for any of them, and a guard test fails if one
// appears. Merging and posting an agent's review are steps of their own (Merge it, Post
// the agent's review), held in by their own checks, never GitHub actions.
//
// It is a leaf: it imports only the item, trust, sensitive, and bump packages, for the
// kinds of item, the trust classes, the sensitive categories, and the ecosystems its
// choices name, and textutil.
package catalog
