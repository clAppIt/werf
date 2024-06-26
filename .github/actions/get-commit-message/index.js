const core = require('@actions/core');
const github = require('@actions/github');
const { graphql } = require("@octokit/graphql");

async function getCommitMessagesFromPullRequest() {
  const { context } = github;

  const repositoryOwner = context.payload.repository.owner.login;
  const repositoryName = context.payload.repository.name;
  const pullRequestNumber = context.payload.pull_request.number;

  core.debug('Geting messages from the pull request...');
  core.debug(` - repositoryOwner: ${repositoryOwner}`);
  core.debug(` - repositoryName: ${repositoryName}`);
  core.debug(` - pullRequestNumber: ${pullRequestNumber}`);

  const graphqlWithAuth = graphql.defaults({
    headers: {
      authorization: `token ${process.env.GITHUB_TOKEN}`,
    },
  });

  const query = `
  query commitMessages(
    $repositoryOwner: String!
    $repositoryName: String!
    $pullRequestNumber: Int!
    $numberOfCommits: Int = 100
  ) {
    repository(owner: $repositoryOwner, name: $repositoryName) {
      pullRequest(number: $pullRequestNumber) {
        commits(last: $numberOfCommits) {
          edges {
            node {
              commit {
                message
              }
            }
          }
        }
      }
    }
  }
`;
  const variables = {
    repositoryOwner,
    repositoryName,
    pullRequestNumber,
  };

  core.debug(` - query: ${query}`);
  core.debug(` - variables: ${JSON.stringify(variables, null, 2)}`);

  const { repository } = await graphqlWithAuth(query, variables);

  core.debug(` - response: ${JSON.stringify(repository, null, 2)}`);

  let messages = [];

  if (repository.pullRequest) {
    messages = repository.pullRequest.commits.edges.map((edgeItem) => edgeItem.node.commit.message);
  }

  return messages;
}

/**
 * Gets all commit messages of a push or title and body of a pull request
 * concatenated to one message.
 *
 * @returns string[]
 */
async function getCommitMessages() {
  const { context } = github;

  const ignoreTitle = core.getInput('ignoreTitle').trim() === 'true';
  const ignoreDescription = core.getInput('ignoreDescription').trim() === 'true';
  const ignoreLatestCommitMessage = core.getInput('ignoreLatestCommitMessage').trim() === 'true';

  const messages = [];
  switch (context.eventName) {
    case 'pull_request': {
      if (!context.payload) {
        throw new Error('No payload found in the context.');
      }

      if (!context.payload.pull_request) {
        throw new Error('No pull_request found in the payload.');
      }

      let message = '';

      if (!ignoreTitle) {
        if (!context.payload.pull_request.title) {
          throw new Error('No title found in the pull_request.');
        }

        message += context.payload.pull_request.title;
      } else {
        core.debug(`Skip getting title, as ignoreTitle is ${ignoreTitle}.`);
      }

      if (!ignoreDescription) {
        if (context.payload.pull_request.body) {
          message = message.concat(
            message !== '' ? '\n\n' : '',
            context.payload.pull_request.body,
          );
        }
      } else {
        core.debug(`Skip getting description, as ignoreDescription is ${ignoreDescription}.`);
      }

      if (message) {
        messages.push(message);
      }

      if (!ignoreLatestCommitMessage) {
        if (!context.payload.pull_request.number) {
          throw new Error('No number found in the pull_request.');
        }

        if (!context.payload.repository) {
          throw new Error('No repository found in the payload.');
        }

        if (!context.payload.repository.name) {
          throw new Error('No name found in the repository.');
        }

        if (
          !context.payload.repository.owner
          || (!context.payload.repository.owner.login
            && !context.payload.repository.owner.name)
        ) {
          throw new Error('No owner found in the repository.');
        }

        core.debug('Trying to get message from the pull request...');

        const commitMessages = await getCommitMessagesFromPullRequest({ github, context, core });
        message = commitMessages[commitMessages.length - 1];
        messages.push(messages.length > 0 ? ''.concat('\n\n', message) : message);
      } else {
        core.debug(`Skip getting commit message, as ignoreLatestCommitMessage is ${ignoreLatestCommitMessage}.`);
      }

      break;
    }
    case 'push': {
      if (!context.payload) {
        throw new Error('No payload found in the context.');
      }

      if (
        !context.payload.commits
        || !context.payload.commits.length
      ) {
        core.debug(`Skip getting commit messages, as context.payload.commits is empty.`);
        break;
      }

      const { message } = context.payload.commits[
        context.payload.commits.length - 1
      ];
      messages.push(message);

      break;
    }
    default: {
      core.info(`Event "${context.eventName}" is not supported.`);
    }
  }

  return messages;
}

async function run() {
  try {
    const messages = await getCommitMessages();

    let commitMessage = '';
    if (messages && messages.length === 0) {
      core.info('No commits found in the payload, skipping check.');
    } else {
      commitMessage = messages.join('\n').replace(/"/gi, '\\"');
      core.info(`Commit messages found:\n ${messages}`);
    }
    core.setOutput('message', commitMessage);
  } catch (error) {
    core.setFailed(error);
  }
}
run();
