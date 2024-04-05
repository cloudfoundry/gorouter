
> [!IMPORTANT]
> Content in this directory is managed by the CI task `sync-dot-github-dir`.

Changing templates
---------------
These templates are synced from [these shared templates](https://github.com/cloudfoundry/wg-app-platform-runtime-ci/tree/main/shared/github).
Each pipeline will contain a `sync-dot-github-dir-*` job for updating the content of these files.
If you would like to modify these, please change them in the shared group.
It's also possible to override the templates on pipeline's parent directory by introducing a custom
template in `$PARENT_TEMPLATE_DIR/github/FILENAME`  or `$PARENT_TEMPLATE_DIR/github/REPO_NAME/FILENAME` in CI repo
