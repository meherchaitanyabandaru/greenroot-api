// Package imagepolicy holds the rules every uploaded image must meet, per
// category (selfie, market ad, licence...): size in pixels, file size,
// formats and the compression quality the apps use. Admins tune the rules
// within fixed safe bounds; the API checks every upload against them, so the
// apps are never the only guard.
package imagepolicy
