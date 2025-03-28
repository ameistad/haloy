git fetch --tags && for tag in $(git tag); do
  git push --delete origin "$tag"
done

