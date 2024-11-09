# immich-sync
`Work in progress, any feedback is appreciated. Has not been tested on a range of systems, and is subject to significant change.`

A background service to sync images from a computer to an Immich server. Similar to the sync provided by the Immich App, immich-sync will watch for any changes in provided folders and upload them to your specified immich instance. 

## Features
- Sync from any folders to your Immich server
- Recursive directory watches for uploads
- Runs as a background service

## How to use it
Immich-sync runs as a background service using `kardianos/service` and can be managed through command line options when running the tool.

When first running the service configure the server URL, API Key, and the paths to sync using the command line arguments
``` shell
./immich-sync --service install --server URL --path FULLPATH --path FULLPATH2 --key APIKEY
```

``` shell
./immich-sync --service start
```

Editing the configuration file can be done by running it again with the updated command line variables or by directly editing the config yaml file located at `$UserConfigDir`/immich-sync/config.yaml. After updating the configuration the service needs to be restarted.

UserConfigDir location defined by Go
- MacOS: $HOME/Library/Application Support
- Windows: %AppData%
- Unix: $XDG_CONFIG_HOME as specified by https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html




Logs are outputted to /tmp/immich-sync.out.log and /tmp/immich-sync.err.log capturing file changes and uploads. 

#### Recovering from network disruptions
During connectivity loss to the Immich server updated files are stored in a buffer which is then synced when connectivity is re-established.

### Command Line Arguments
 - --service: Control the system service [start, stop, restart, install, uninstall]
 - --server: URL For immich server to make api calls
 - --path: Add path to folder to sync, stack with multiple calls for multiple paths
 - --key: API Key to server
