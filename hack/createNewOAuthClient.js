/** 
 * This script helps to generate OAuth client id and secret for a Turbonomic instance
 * Note: The script is suppose to run within the browser console
 * 
 * To successfully execute this script you need to:
 *  1. Making sure your Turbonomic OAuth supported
 *  2. Login to your Turbonomic instance
 *  3. Open your browser console while in the Turbonomic UI
 *  4. Copy and paste the following script to you browser console
 *  5. Modify fields that marked "<-- change if needed"
 *  6. Execute the command by hitting 'Enter' key
*/

(oauthClientSettings => fetch(location.origin + "/vmturbo/rest/authorization/oauth2/clients", {
    method: "POST",
    headers: {"accept": "application/json", "Content-type": "application/json; charset=UTF-8"},
    body: JSON.stringify(oauthClientSettings)
}).then(resp => {
    const contentType = resp.headers.get('Content-Type');
    let respData = (contentType && contentType.includes('application/json'))? resp.json() : resp.text()
    if (!resp.ok) {
        return respData.then(errorData => {
            throw new Error(resp?.status + ": " + (errorData?.message || resp?.statusText));
        });
    }
    return respData;
}).then(respData => {
    console.log("Here is your OAuth id and secret: ");
    console.log("clientId:     "+ respData?.clientId);
    console.log("clientSecret: "+ respData?.clientSecret);
}).catch(e => console.error(String(e))
))({
    clientName: "oauth2_client",                           // <-- change if needed
    grantTypes: ["client_credentials"],                    // <-- change if needed
    clientAuthenticationMethods: ["client_secret_post"],  // <-- change if needed
    scopes: ["role:PROBE_ADMIN"],                           // <-- change if needed
    tokenSettings: {accessToken: {ttlSeconds: 600}}
});