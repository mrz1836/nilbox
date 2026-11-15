import type { AgentInfo, Profile } from '../platform'

// loginCommand is the command that signs an agent in with a profile: the agent's login,
// run with its config variable set to the profile's folder for any profile but the
// default.
export function loginCommand(agent: AgentInfo, profile: Profile): string {
  return profile.configDir === ''
    ? `${agent.command} login`
    : `${agent.configEnv}=${profile.folder} ${agent.command} login`
}
