# How Suggestions Work

mnp.tips suggests who to send against the other team's picks, and what to pick
yourself. This explains how it decides, so you know when to trust it and when to
overrule it.

## Guessing a Player's Score

Everything starts with a guess at what each player will score on each machine.
Pinball scores vary enormously, so the guess is a range rather than one number.
In half of their games a player scores between about half and nearly double
their typical score.

To guess someone's typical score on a machine, mnp.tips starts from the league's
typical score on it and adjusts it for their IPR. An IPR 6 typically scores
about two and a half times the league's typical score, and an IPR 1 about two
thirds of it. As a player's league games build up, the guess leans more on what
they've actually scored:

1. **Across every machine.** Someone who usually scores 50% more than the
   league's typical score probably does the same on a machine they rarely play.
   After about 20 league games, this counts for more than their IPR.
2. **On this machine.** After about 40 games on it, a player's own scores count
   for more than everything else. A few games say less than you'd think, and one
   great night on Venom doesn't make someone a Venom specialist.

It also knows three habits of league play. Players score about 8% more at home,
6% more on machines their team picked, and 11% less on machines they've never
played in the league.

Older scores count for less, since players improve and software updates change
scoring. A score from two years ago counts half as much as one from this season.

## Turning Scores Into Points

With a range for each player, mnp.tips works out how often one outscores
another. In singles it also works out how often the higher score doubles the
lower, which gets 3 points instead of 2. In doubles it works out each of the
four head-to-heads, plus which pair's combined score is higher. Together these
give the points each side can expect from a game on average, and each side's
chance of taking most of them.

## Responding

When the other team picks, mnp.tips tries every way of sending your players to
their games, each player once. It suggests whichever gets the most points across
the whole round, which isn't always the obvious choice. Say your strongest
player would win game 1 easily and game 2 narrowly. If a teammate could also win
game 1 but nobody else could win game 2, your strongest player goes to game 2.

It also makes sure everyone can still play 3 games, so you keep your match bonus
points. Anyone who sat out earlier has to play. In a full lineup, nobody who's
played 3 games can play a 4th.

A game marked as a clear pick is one where sending anyone else would cost the
round a quarter of a point or more. For most games the alternatives are close,
so you can choose between them for any reason you like.

## Picking

Picking is harder, because the other team responds to whatever you pick. So
mnp.tips assumes they'll respond as well as they can, exactly as mnp.tips would
suggest to their captain. The "likely to send" players are that response.

There are billions of possible picks at a 26-machine venue, far too many to try.
Instead it starts from many random picks and improves each a change at a time
until no change helps. At 8- and 9-machine venues, where trying every pick is
possible, this found the top pick in all 17 matchups checked. It's less certain
at big venues, where there's no way to check.

On the page you can untick machines and players or add subs. Changing any game
pins it, and the other picks fit around it.

I checked ten rounds closely, at venues with 8 to 47 machines. After each change
I let the other picks fit around it. Unticking one of the suggested machines
lost 0.02 to 0.08 points on average, and pinning a singles player to a
different suggested machine lost 0.05 to 0.23. Arranging the same players on
the same machines at random, with nothing fitting around it, lost 0.25 to 1
point. So overriding a suggestion or two gives up little, as long as you let the
page fit the rest around your choice.

## How Much to Trust It

I tuned the guesses on league games from fall 2022 to fall 2025, then checked
them on 2026's games, which they hadn't seen. In head-to-heads, players it gave
a 60%, 70%, 80% or 90% chance to win won about that often. It predicts results
a little better than IPR alone.

Trust it least for new players and subs with few league games, whose guess is
mostly their IPR. Picks also assume the other captain responds perfectly, so
against one who doesn't you may do better than the suggestion.

These are deliberately left out, because they didn't help predict who wins:

- **How someone has played earlier tonight.** Their later scores do track it,
  but mostly because a machine runs high or low that night for everyone on it,
  so it doesn't change who wins.
- **Which venue.** Players who've played a machine at this venue don't score
  higher than players who've played it elsewhere.
- **How swingy each machine is.** Some machines vary far more than others, but
  accounting for it made predictions worse.

mnp.tips can't know who's tired, who just learned a new strategy, or which
flipper is weak tonight. That's when to overrule it.
