# immich-sync

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

Editing the configuration file can be done by running it again with the updated command line variables or by directly editing the config yaml file located at `$UserConfigDir`/immich-sync/config.yaml. After udpating the configuration the service needs to be restarted.

UserConfigDir location defined by Go
```
On Unix systems, it returns $XDG_CONFIG_HOME as specified by https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html if non-empty, else $HOME/.config. On Darwin, it returns $HOME/Library/Application Support. On Windows, it returns %AppData%. On Plan 9, it returns $home/lib.
```

### Command Line Arguments
 --service: Control the system service [start, stop, restart, install, uninstall]
 --server: URL For immich server to make api calls
 --path: Add path to folder to sync, stack with multiple calls for multiple paths
 --key: API Key to server
