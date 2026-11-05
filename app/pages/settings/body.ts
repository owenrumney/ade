// The Settings form, as markup, so both hosts draw the one page. No
// script tag and no stylesheet: the paths differ per host.
export const BODY = `
    <main>
      <h1>Settings</h1>
      <p id="path"></p>
      <div id="status" role="status"></div>
      <form id="settings">
        <section><h2>Repositories</h2><label>Scan directories<textarea id="scan-dirs" rows="4"></textarea></label><label>Ignored directories<textarea id="ignored-dirs" rows="3"></textarea></label></section>
        <section><h2>Instructions</h2><label>Reviews, commit messages and My Day<textarea id="instructions" rows="7"></textarea></label><p id="instruction-limit"></p></section>
        <section><h2>Dashboard</h2><label>URL<input id="dashboard-url" type="url" placeholder="http://pi.local:8080" /></label><label>Machine name<input id="dashboard-host" /></label><label>Claude account<input id="dashboard-claude" /></label><label>Codex account<input id="dashboard-codex" /></label><div class="secret"><label>Token <span id="dashboard-token-state"></span><input id="dashboard-token" type="password" autocomplete="off" /></label><button type="button" data-secret="dashboard-token">Save token</button><button type="button" data-forget="dashboard-token">Forget</button></div></section>
        <section><h2>Integrations</h2><div class="secret"><label>Linear API key <span id="linear-key-state"></span><input id="linear-key" type="password" autocomplete="off" /></label><button type="button" data-secret="linear-key">Save key</button><button type="button" data-forget="linear-key">Forget</button></div><label>Home Assistant URL<input id="home-assistant-url" type="url" /></label><label>Desk light<input id="home-assistant-desk" placeholder="light.desk" /></label><label>Notify service<input id="home-assistant-notify" placeholder="notify.mobile_app" /></label><div class="secret"><label>Home Assistant token <span id="home-assistant-token-state"></span><input id="home-assistant-token" type="password" autocomplete="off" /></label><button type="button" data-secret="home-assistant-token">Save token</button><button type="button" data-forget="home-assistant-token">Forget</button></div><div class="secret"><label>GitHub Packages token <span id="github-packages-token-state"></span><input id="github-packages-token" type="password" autocomplete="off" /></label><button type="button" data-secret="github-packages-token">Save token</button><button type="button" data-forget="github-packages-token">Forget</button></div></section>
        <button id="save" type="submit">Save settings</button>
      </form>
    </main>
`;
