package procs

// In returns the processes whose working directory is dir itself, not one inside it, by
// PID, as a Located that has resolved nothing yet answers: what the tests compare a
// Located asked many times with. The app asks a Located, which resolves each process's
// directory once for every folder it is asked of.
func In(list []Process, dir string) []Process {
	return Locate(list).In(dir)
}
